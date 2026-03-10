package notifier

import (
	"testing"
)

func TestGetPairsText(t *testing.T) {
	type testCase struct {
		name     string   // название проверки, чтобы понимать что сломалось
		input    []string //то, что дадим функции
		expected string   // ожидаемый результат функции
	}

	//заполнение структуры примерами
	tests := []testCase{
		{
			name:     "пустой список",
			input:    []string{},
			expected: "All",
		},
		{
			name:     "Две монеты",
			input:    []string{"BTCUSDT", "ETHUSDT"},
			expected: "BTCUSDT, ETHUSDT",
		},
		{
			name:     "Шесть монет (больше пяти)",
			input:    []string{"1", "2", "3", "4", "5", "6"},
			expected: "6 пар",
		},
	}

	//цикл который будет проходиться по структуре теста
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			//вызов функции
			result := getPairsText(tc.input)

			//проверка результата
			if result != tc.expected {
				t.Errorf("ОШИБКА в тесте '%s': ожидали '%s', а получили '%s'", tc.name, tc.expected, result)
			}
		})
	}
}
