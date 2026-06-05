package market

import "strings"

// CNStockSecID 东方财富 A 股 secid。
func CNStockSecID(code string) string {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return ""
	}
	switch {
	case strings.HasPrefix(code, "6"), strings.HasPrefix(code, "688"), strings.HasPrefix(code, "689"):
		return "1." + code
	case strings.HasPrefix(code, "000300"), strings.HasPrefix(code, "000016"), strings.HasPrefix(code, "000905"):
		return "1." + code
	default:
		return "0." + code
	}
}

// USStockSecID 东方财富美股 secid（先 NYSE/NASDAQ，ETF 常用 107）。
func USStockSecID(code string) []string {
	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return nil
	}
	return []string{"105." + code, "107." + code, "106." + code}
}

func inferMarket(code string) string {
	code = strings.TrimSpace(strings.ToUpper(code))
	if len(code) == 6 && isDigits(code) {
		return "cn"
	}
	if len(code) <= 5 && code != "" {
		return "us"
	}
	return ""
}
