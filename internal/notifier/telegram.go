package notifier

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/wthareutalking/crypto-arbitrage/internal/models"
	"github.com/wthareutalking/crypto-arbitrage/internal/storage/postgres"
	"github.com/wthareutalking/crypto-arbitrage/pkg/storage"
	"go.uber.org/zap"
)

type Notifier struct {
	bot          *tgbotapi.BotAPI
	adminID      int64
	pgStorage    *postgres.Storage
	redisStorage *storage.Storage

	validPairs map[string]bool //O(1)
	logger     *zap.Logger
}

func NewNotifier(token string, adminID int64, pgStorage *postgres.Storage, redisStorage *storage.Storage, systemPairs []string, logger *zap.Logger) (*Notifier, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create new bot: %w", err)
	}

	validMap := make(map[string]bool, len(systemPairs))
	for _, p := range systemPairs {
		validMap[strings.ToUpper(p)] = true
	}

	return &Notifier{
		bot:          bot,
		adminID:      adminID,
		pgStorage:    pgStorage,
		redisStorage: redisStorage,
		validPairs:   validMap,
		logger:       logger,
	}, nil
}

func (n *Notifier) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := n.bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil && update.Message.IsCommand() {
			n.handleCommand(ctx, update.Message)
			continue
		}

		if update.Message != nil && !update.Message.IsCommand() {
			state, _ := n.redisStorage.GetState(ctx, update.Message.From.ID)

			if state == "waiting_for_spread" {
				n.handleSpreadInput(ctx, update.Message)
				continue
			}

			if state == "waiting_for_add_pairs" {
				n.handlePairsInput(ctx, update.Message, "add")
				continue
			}

			if state == "waiting_for_remove_pairs" {
				n.handlePairsInput(ctx, update.Message, "remove")
				continue
			}
		}

		if update.CallbackQuery != nil {
			n.handleCallback(ctx, update.CallbackQuery)
			continue
		}
	}
}

func (n *Notifier) handlePairsInput(ctx context.Context, msg *tgbotapi.Message, action string) {
	defer n.redisStorage.DelState(ctx, msg.From.ID)

	text := strings.TrimSpace(msg.Text)
	rawPairs := strings.Split(text, ",")

	// Собираем то, что прислал юзер (с валидацией)
	var inputPairs []string
	for _, p := range rawPairs {
		cleanPair := strings.ToUpper(strings.TrimSpace(p))
		if cleanPair == "" {
			continue
		}
		// Проверка на валидность (существует ли вообще такая пара на биржах)
		// if ok := n.validPairs[cleanPair]; !ok {
		// 	n.sendText(msg.Chat.ID, fmt.Sprintf("⚠️ The %s pair is not supported by the system.", cleanPair))
		// 	continue
		// }
		inputPairs = append(inputPairs, cleanPair)
	}

	if len(inputPairs) == 0 {
		n.sendText(msg.Chat.ID, "❌ No valid pair found. The operation has been cancelled.")
		return
	}

	// Достаем текущего юзера
	user, err := n.pgStorage.GetUser(ctx, msg.From.ID)
	if err != nil {
		n.sendText(msg.Chat.ID, "❌ DB error.")
		return
	}

	currentPairs := user.Settings.Pairs

	// Защита от "Все": если у юзера стояло "Все" (пустой массив), мы создаем новый
	if len(currentPairs) == 0 && currentPairs == nil {
		currentPairs = []string{}
	}
	// ЛОГИКА ДОБАВЛЕНИЯ
	if action == "add" {
		// Ограничение для защиты от спама
		maxPairs := 20
		if len(currentPairs) >= maxPairs {
			n.sendText(msg.Chat.ID, fmt.Sprintf("❌ Portfolio limit reached (%d pairs).", maxPairs))
			return
		}
		// Добавление новых пар, проверка на дубликаты
		addedCount := 0
		var successfullyAdded []string
		var cleanedPairs []string

		for _, existing := range currentPairs {
			if existing != "STOP_ALL" {
				cleanedPairs = append(cleanedPairs, existing)
			}
		}
		currentPairs = cleanedPairs

		for _, newPair := range inputPairs {
			exists := false
			for _, existingPair := range currentPairs {
				if newPair == existingPair {
					exists = true
					break
				}
			}
			if !exists {
				currentPairs = append(currentPairs, newPair)
				successfullyAdded = append(successfullyAdded, newPair)
				addedCount++
			}
		}
		if addedCount > 0 {
			msgText := fmt.Sprintf("✅ New pairs added: %d", addedCount)
			n.updateUserPairs(ctx, msg.Chat.ID, msg.From.ID, currentPairs, msgText)

			for _, p := range successfullyAdded {
				if err := n.redisStorage.PuiblishNewPair(ctx, p); err != nil {
					n.logger.Error("Failed to publish pair to scanners", zap.Error(err), zap.String("pair", p))
				} else {
					n.logger.Info("Published new pair to scanners", zap.String("pair", p))
				}
			}
		} else {
			n.sendText(msg.Chat.ID, "ℹ️ All these pairs are already in your portfolio.")
		}
	}

	// ЛОГИКА УДАЛЕНИЯ
	if action == "remove" {
		var newPairs []string
		removedCount := 0
		// Проходим по текущим парам. Если пары НЕТ в списке на удаление - оставляем ее.
		for _, existingPair := range currentPairs {
			shouldRemove := false
			for _, pairToRemove := range inputPairs {
				if existingPair == pairToRemove {
					shouldRemove = true
					removedCount++
					break
				}
			}
			if !shouldRemove {
				newPairs = append(newPairs, existingPair)
			}
		}
		msgText := fmt.Sprintf("🗑 Deleted pairs: %d", removedCount)
		n.updateUserPairs(ctx, msg.Chat.ID, msg.From.ID, newPairs, msgText)
	}
}

func (n *Notifier) handleCallback(ctx context.Context, callback *tgbotapi.CallbackQuery) {
	defer n.bot.Request(tgbotapi.NewCallback(callback.ID, ""))

	switch callback.Data {
	case "cmd_set_spread":
		n.sendText(callback.Message.Chat.ID, "✍️ Enter the spread (for example: 1.5):")
		n.redisStorage.SetStage(ctx, callback.From.ID, "waiting_for_spread")

	case "cmd_set_pairs":
		user, err := n.pgStorage.GetUser(ctx, callback.From.ID)
		if err != nil {
			n.sendText(callback.Message.Chat.ID, "❌ Error. Press /start")
			return
		}
		n.sendPortfolioMenu(callback.Message.Chat.ID, user)

	case "cmd_add_pairs":
		// Переводим в стейт "добавления"
		n.sendText(callback.Message.Chat.ID, "➕ *Addition*\nSend tickers separated by a comma (for example: BTCUSDT, ETHUSDT):")
		n.redisStorage.SetStage(ctx, callback.From.ID, "waiting_for_add_pairs")

	case "cmd_remove_pairs":
		// Переводим в стейт "удаления"
		n.sendText(callback.Message.Chat.ID, "➖ *Delete *\nSend the tickers you want to delete from the list:")
		n.redisStorage.SetStage(ctx, callback.From.ID, "waiting_for_remove_pairs")

	case "cmd_track_all":
		// Быстрая команда: Очищаем список (означает "Следить за всем")
		n.updateUserPairs(ctx, callback.Message.Chat.ID, callback.From.ID, []string{"ALL"}, "✅ Now you can track ALL market pairs.")

	case "cmd_clear_all":
		// Быстрая команда: Записываем несуществующую монету (хак), чтобы бот замолчал
		n.updateUserPairs(ctx, callback.Message.Chat.ID, callback.From.ID, []string{}, "🛑 Tracking has been stopped. You will not receive any signals.")

	case "cmd_back_settings":
		// Кнопка Назад
		user, _ := n.pgStorage.GetUser(ctx, callback.From.ID)
		n.sendSettingsMenu(callback.Message.Chat.ID, user)
	}
}

func (n *Notifier) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		err := n.pgStorage.SaveUser(ctx, msg.From.ID, msg.From.UserName)
		if err != nil {
			n.sendText(msg.Chat.ID, "❌ Registration error")
			return
		}
		n.sendText(msg.Chat.ID, "👋 Тестовое приветствие *потом отредактировать* ")

	case "status":
		user, err := n.pgStorage.GetUser(ctx, msg.From.ID)
		if err != nil {
			n.sendText(msg.Chat.ID, "You are not registered. Press /start")
			return
		}
		status := "Free"
		if user.SubscriptionEnd.After(time.Now()) {
			status = "Premium (until " + user.SubscriptionEnd.Format("02.01.2006") + ")"
		}
		n.sendText(msg.Chat.ID, "Your status: "+status)
	case "settings":
		user, err := n.pgStorage.GetUser(ctx, msg.From.ID)
		if err == nil {
			n.sendSettingsMenu(msg.Chat.ID, user)
		} else {
			n.sendText(msg.Chat.ID, "First press /start")
		}
	}
}

func (n *Notifier) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	n.bot.Send(msg)
}

func (n *Notifier) BroadcastSignal(signal models.ArbitrageSignal) {
	ctx := context.Background()

	users, err := n.pgStorage.GetUsers(ctx)
	if err != nil {
		n.logger.Error("Failed to get users for broadcast", zap.Error(err))
		return
	}
	//ФАЗА ФИЛЬТРАЦИИ: Собираем тех, кому сигнал подходит по настройкам
	var eligibleUsers []postgres.User // те кто прошел проверку
	var eligibleUserIDs []int64       //их айди для redis

	for _, user := range users {
		isPremium := user.SubscriptionEnd.After(time.Now())

		if signal.Spread > 0.6 && !isPremium {
			continue // фильтр premium
		}

		if signal.Spread < user.Settings.MinSpread {
			continue // фильр минимального спреда
		}

		//проверка на СТОП (если массив пустой - юзер нажал Clear, ему ничего не шлем!)
		if len(user.Settings.Pairs) == 0 {
			continue
		}

		//проверка на ВСЁ (если в массиве есть слово "ALL" - шлем ему любые сигналы)
		wantsAll := false
		for _, p := range user.Settings.Pairs {
			if p == "ALL" {
				wantsAll = true
				break
			}
		}

		//Проверка на КОНКРЕТНУЮ пару (если он не выбрал ALL)
		wantsPair := false
		if !wantsAll {
			for _, p := range user.Settings.Pairs {
				if p == signal.Pair {
					wantsPair = true
					break
				}
			}
		}
		// Если юзер не хочет ВСЁ и не хочет ЭТУ КОНКРЕТНУЮ пару - пропускаем его
		if !wantsAll && !wantsPair {
			continue
		}

		// Если юзер прошел все проверки - добавляем его в список "кандидатов на отправку"
		eligibleUsers = append(eligibleUsers, user)
		eligibleUserIDs = append(eligibleUserIDs, user.ID)
	}
	if len(eligibleUsers) == 0 {
		return
	}

	// ФАЗА БЛОКИРОВКИ: Идем в Redis одним запросом для всех кандидатов
	// Ставим персональный замок на 30 секунд
	locks, err := n.redisStorage.SetAlertLocksBulk(ctx, eligibleUserIDs, signal.Pair, 30*time.Second)
	if err != nil {
		n.logger.Error("Failed to bulk lock in Redis", zap.Error(err))
		return
	}

	text := fmt.Sprintf(
		"🚀 *ARBITRAGE FOUND!* 🚀\n\n"+
			"Pair: *%s*\n"+
			"Spread: *%.2f%%*\n"+
			"Direction: %s\n"+
			"Buy: `%.4f` | Sell: `%.4f`",
		signal.Pair, signal.Spread, signal.Direction, signal.BuyPrice, signal.SellPrice,
	)
	msg := tgbotapi.NewMessage(0, text)
	msg.ParseMode = "Markdown"

	// ФАЗА ОТПРАВКИ: Рассылаем только тем, кто получил 'true' от Redis
	for _, user := range eligibleUsers {
		if canSend := locks[user.ID]; canSend {
			msg.ChatID = user.ID
			_, err = n.bot.Send(msg)
			if err != nil {
				n.logger.Error("Failed to send message", zap.Int64("user_id", user.ID), zap.Error(err))
			}
		}
	}
}

func (n *Notifier) sendSettingsMenu(chatID int64, user *postgres.User) {
	text := fmt.Sprintf(
		"⚙️ *Settings*\n\n"+
			"📉 Min. spread: *%.1f%%*\n"+
			"Pairs: *%s*",
		user.Settings.MinSpread,
		getPairsText(user.Settings.Pairs),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Change spread", "cmd_set_spread"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💼 Currency Pair Manager (Portfolio)", "cmd_set_pairs"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard //прикрепляем кнопки
	n.bot.Send(msg)
}

func (n *Notifier) sendPortfolioMenu(chatID int64, user *postgres.User) {
	var text string
	if len(user.Settings.Pairs) == 1 && user.Settings.Pairs[0] == "ALL" {
		text = "📊 Your portfolio\n\nCurrently, you are tracking: *ALL pairs*..."
	} else if len(user.Settings.Pairs) == 0 {
		text = "🔴 Tracking is STOPPED. Your portfolio is empty."
	} else {
		// Ограничиваем вывод, если пар слишком много (чтобы сообщение не было огромным)
		displayPairs := user.Settings.Pairs
		hiddenCount := 0
		if len(displayPairs) > 10 {
			hiddenCount = len(displayPairs) - 10
			displayPairs = displayPairs[:10]
		}

		text = fmt.Sprintf("📊 *Your portfolio* (Quantity: %d)\n\nTracked pairs:\n`%s`",
			len(user.Settings.Pairs),
			strings.Join(displayPairs, ", "),
		)

		if hiddenCount > 0 {
			text += fmt.Sprintf("\n... and %d more hidden", hiddenCount)
		}
	}

	//формируем клавиатурууправления
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add pairs ", "cmd_add_pairs"),
			tgbotapi.NewInlineKeyboardButtonData("➖ Remove pairs", "cmd_remove_pairs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🌐 Track ALL", "cmd_track_all"),
			tgbotapi.NewInlineKeyboardButtonData("🗑 Clear (Stop)", "cmd_clear_all"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Back to settings", "cmd_back_settings"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	n.bot.Send(msg)
}

func getPairsText(pairs []string) string {
	if len(pairs) == 0 {
		return "All"
	}
	if len(pairs) > 5 {
		return fmt.Sprintf("%d пар", len(pairs))
	}
	return strings.Join(pairs, ", ")
}

// updateUserPairs - утилита для быстрого обновления списка пар в БД
func (n *Notifier) updateUserPairs(ctx context.Context, chatID int64, userID int64, newPairs []string, succsessMsg string) {
	//достаем юзера
	user, err := n.pgStorage.GetUser(ctx, userID)
	if err != nil {
		n.sendText(chatID, "❌ Error getting user.")
		return
	}
	//обновление структуры
	user.Settings.Pairs = newPairs

	//сохранение в бд
	err = n.pgStorage.UpdateSettings(ctx, user.ID, user.Settings)
	if err != nil {
		n.sendText(chatID, "❌ Error saving settings.")
		return
	}
	n.sendText(chatID, succsessMsg)
	// повторная отрисовка меню портфеля, чтобы юзер увидел изменения
	n.sendPortfolioMenu(chatID, user)
}

// handleSpreadInput обрабатывает текстовый ввод пользователя, когда он меняет минимальный спред
func (n *Notifier) handleSpreadInput(ctx context.Context, msg *tgbotapi.Message) {
	//Изменение запятой на точку, если юзер ввел "1,5" вместо "1.5"
	text := strings.ReplaceAll(msg.Text, ",", ".")

	val, err := strconv.ParseFloat(text, 64)

	if err != nil || val < 0 {
		n.sendText(msg.Chat.ID, "❌ Error! Enter a positive number (for example 0.5). Try again:")
		return
	}

	user, err := n.pgStorage.GetUser(ctx, msg.From.ID)
	if err == nil {
		user.Settings.MinSpread = val
		n.pgStorage.UpdateSettings(ctx, user.ID, user.Settings)
	}

	//Сбрасывание стейт (FSM) в Redis
	n.redisStorage.DelState(ctx, msg.From.ID)

	n.sendText(msg.Chat.ID, fmt.Sprintf("✅ Done! The minimum spread has been changed to: %.2f%%", val))
}
