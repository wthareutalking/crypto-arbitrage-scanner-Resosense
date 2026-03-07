package exchange

type PriceUpdate struct {
	Symbol    string //название валютной пары
	Bid       float64
	Ask       float64
	Timestamp int64
	Exchange  string //название биржи
}

type Exchange interface {
	Connect(pairs []string) error
	SubscribeDynamic(pairs []string) error
	StreamPrices() <-chan PriceUpdate
	Close() error
}
