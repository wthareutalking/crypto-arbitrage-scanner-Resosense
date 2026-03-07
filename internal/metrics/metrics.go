package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// priceUpdates считает сколько цен пришло с биржи
var PriceUpdates = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "resosense_price_updates_total",
	Help: "Total price updates",
}, []string{"exchange", "pair"})

// arbitrageFound считает найденные сигналы
var ArbitrageFound = promauto.NewCounter(prometheus.CounterOpts{
	Name: "resosense_arbitrage_found_total",
	Help: "Total arbitrage signals",
})

var ActiveUsers = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "resosense_active_users",
	Help: "Current number of users in DB",
})
