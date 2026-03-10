package comparator

import (
	"testing"
)

func TestCalculateSpread(t *testing.T) {
	type testCase struct {
		name     string
		buy      float64
		sell     float64
		expected float64
	}

	tests := []testCase{
		{
			name:     "Стандартный профит",
			buy:      100.0,
			sell:     105.0,
			expected: 5.0, // (105-100)/100*100 = 5%
		},
		{
			name:     "Убыток",
			buy:      100.0,
			sell:     90.0,
			expected: -10.0,
		},
		{
			name:     "защита от нуля (buy = 0)",
			buy:      0.0,
			sell:     100.0,
			expected: 0.0,
		},
		{
			name:     "защита от нуля (sell = 0)",
			buy:      100.0,
			sell:     0.0,
			expected: 0.0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalculateSpread(tc.buy, tc.sell)
			if result != tc.expected {
				t.Errorf("ОШИБКА '%s': ожидали %v, получили %v", tc.name, tc.expected, result)
			}
		})
	}
}
