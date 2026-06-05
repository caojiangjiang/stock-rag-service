package portfolio

import "strings"

func normalizeAssetType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case AssetFund:
		return AssetFund
	default:
		return AssetStock
	}
}

func positionKey(assetType, code string) string {
	return normalizeAssetType(assetType) + ":" + strings.TrimSpace(code)
}
