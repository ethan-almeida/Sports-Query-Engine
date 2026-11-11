package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const (
	MaxSportLen    = 32
	MaxQuestionLen = 2000
	MinRows        = 1
	MaxRows        = 1000
	DefaultRows    = 50
)

type QueryRequest struct {
	Sport    string `json:"sport"`
	Question string `json:"question"`
	MaxRows  int    `json:"max_rows"`
}

type OrchestrateRequest struct {
	Sport    string `json:"sport"`
	Question string `json:"question"`
	MaxRows  int    `json:"max_rows"`
	CacheKey string `json:"cache_key"`
}

type QueryResponse struct {
	Query     string   `json:"query"`
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	Truncated bool     `json:"truncated"`
	ElapsedMS int      `json:"elapsed_ms"`
}

type Error struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewRequest(original json.RawMessage) (QueryRequest, error) {
	if len(original) == 0 {
		return QueryRequest{}, fmt.Errorf("body is empty")
	}
	var request QueryRequest
	decoder := json.NewDecoder(bytes.NewReader(original))
	if err := decoder.Decode(&request); err != nil {
		return QueryRequest{}, fmt.Errorf("body is not valid JSON")
	}
	if request.MaxRows == 0 {
		request.MaxRows = DefaultRows
	}
	if err := request.validate(); err != nil {
		return QueryRequest{}, err
	}
	return request, nil
}

func (request QueryRequest) validate() error {
	if runeLen(request.Sport) < 1 || runeLen(request.Sport) > MaxSportLen {
		return fmt.Errorf("sport must be between 1 and %d characters", MaxSportLen)
	}
	if runeLen(request.Question) < 1 || runeLen(request.Question) > MaxQuestionLen {
		return fmt.Errorf("question must be between 1 and %d characters", MaxQuestionLen)
	}
	if request.MaxRows < MinRows || request.MaxRows > MaxRows {
		return fmt.Errorf("max_rows must be between %d and %d", MinRows, MaxRows)
	}
	return nil
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}

func (response QueryResponse) IsZero() bool {
	return response.Query == "" && response.Columns == nil && response.Rows == nil
}
