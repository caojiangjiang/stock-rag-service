package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var eastMoneyBoardListURL = "https://push2.eastmoney.com/api/qt/clist/get"

// BoardMember 概念/行业板块成分。
type BoardMember struct {
	Symbol         string
	CompanyName    string
	Market         string // cn
	Price          float64
	ChangePercent  float64
	HasQuote       bool
}

type eastMoneyBoardListResp struct {
	Data *struct {
		Total int `json:"total"`
		Diff  []struct {
			Code string `json:"f12"`
			Name string `json:"f14"`
		} `json:"diff"`
	} `json:"data"`
	RC int `json:"rc"`
}

type eastMoneyBoardMembersResp struct {
	Data *struct {
		Total int `json:"total"`
		Diff  []struct {
			Symbol string  `json:"f12"`
			Name   string  `json:"f14"`
			Price  float64 `json:"f2"`
			Change float64 `json:"f3"`
		} `json:"diff"`
	} `json:"data"`
	RC int `json:"rc"`
}

// FetchConceptBoards 拉取东方财富概念板块列表。
func FetchConceptBoards(ctx context.Context, page, pageSize int) ([]struct{ Code, Name string }, int, error) {
	return fetchBoardList(ctx, "m:90+t:3", page, pageSize)
}

// ResolveConceptBoard 按名称关键词匹配概念板块（精确优先，其次包含）。
func ResolveConceptBoard(ctx context.Context, keyword string) (string, string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", "", fmt.Errorf("empty keyword")
	}
	const pageSize = 100
	for page := 1; page <= 6; page++ {
		list, total, err := FetchConceptBoards(ctx, page, pageSize)
		if err != nil {
			return "", "", err
		}
		for _, b := range list {
			if b.Name == keyword {
				return b.Code, b.Name, nil
			}
		}
		for _, b := range list {
			if strings.Contains(b.Name, keyword) {
				return b.Code, b.Name, nil
			}
		}
		if page*pageSize >= total {
			break
		}
		time.Sleep(120 * time.Millisecond)
	}
	return "", "", fmt.Errorf("concept board not found: %s", keyword)
}

func fetchBoardList(ctx context.Context, fs string, page, pageSize int) ([]struct{ Code, Name string }, int, error) {
	u, err := url.Parse(eastMoneyBoardListURL)
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	q.Set("pn", strconv.Itoa(page))
	q.Set("pz", strconv.Itoa(pageSize))
	q.Set("po", "1")
	q.Set("np", "1")
	q.Set("fltt", "2")
	q.Set("invt", "2")
	q.Set("fid", "f14")
	q.Set("fs", fs)
	q.Set("fields", "f12,f14")
	u.RawQuery = q.Encode()

	body, err := eastMoneyGET(ctx, u.String())
	if err != nil {
		return nil, 0, err
	}
	var payload eastMoneyBoardListResp
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, err
	}
	if payload.RC != 0 || payload.Data == nil {
		return nil, 0, fmt.Errorf("board list unavailable")
	}
	out := make([]struct{ Code, Name string }, 0, len(payload.Data.Diff))
	for _, row := range payload.Data.Diff {
		out = append(out, struct{ Code, Name string }{Code: row.Code, Name: row.Name})
	}
	return out, payload.Data.Total, nil
}

// FetchBoardConstituents 拉取板块成分股（A 股）。
func FetchBoardConstituents(ctx context.Context, boardCode string, maxMembers int) ([]BoardMember, error) {
	boardCode = strings.TrimSpace(strings.ToUpper(boardCode))
	if !strings.HasPrefix(boardCode, "BK") {
		return nil, fmt.Errorf("invalid board code: %s", boardCode)
	}
	if maxMembers <= 0 {
		maxMembers = 40
	}

	const pageSize = 100
	var out []BoardMember
	page := 1
	for len(out) < maxMembers {
		u, err := url.Parse(eastMoneyBoardListURL)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("pn", strconv.Itoa(page))
		q.Set("pz", strconv.Itoa(pageSize))
		q.Set("po", "1")
		q.Set("np", "1")
		q.Set("fltt", "2")
		q.Set("invt", "2")
		q.Set("fid", "f3")
		q.Set("fs", "b:"+boardCode)
		q.Set("fields", "f12,f14,f2,f3")
		u.RawQuery = q.Encode()

		body, err := eastMoneyGET(ctx, u.String())
		if err != nil {
			return nil, err
		}
		var payload eastMoneyBoardMembersResp
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload.RC != 0 || payload.Data == nil {
			break
		}
		if len(payload.Data.Diff) == 0 {
			break
		}
		for _, row := range payload.Data.Diff {
			sym := strings.TrimSpace(row.Symbol)
			if sym == "" {
				continue
			}
			out = append(out, BoardMember{
				Symbol:        sym,
				CompanyName:   strings.TrimSpace(row.Name),
				Market:        "cn",
				Price:         row.Price,
				ChangePercent: row.Change,
				HasQuote:      row.Price > 0,
			})
			if len(out) >= maxMembers {
				break
			}
		}
		if page*pageSize >= payload.Data.Total {
			break
		}
		page++
		time.Sleep(80 * time.Millisecond)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no constituents for board %s", boardCode)
	}
	return out, nil
}
