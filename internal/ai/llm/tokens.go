package llm

import (
	"context"

	"nautilus/internal/enums"
)

func SendToken(ctx context.Context, ch chan<- Token, token Token) {
	select {
	case ch <- token:
	case <-ctx.Done():
	}
}

type Token interface {
	TokenType() enums.TokenType
	Content() string
}

type TokenStream interface {
	Tokens() <-chan Token
	Cancel()
}

type TokenStreamFunc func(tokens <-chan Token, cancel context.CancelFunc) TokenStream

type ErrorToken struct {
	Type enums.TokenType
	Err  error
}

func (t *ErrorToken) TokenType() enums.TokenType {
	return t.Type
}

func (t *ErrorToken) Content() string {
	return t.Err.Error()
}

type TextToken struct {
	Type enums.TokenType
	Text string
}

func (t *TextToken) TokenType() enums.TokenType {
	return t.Type
}

func (t *TextToken) Content() string {
	return t.Text
}

type ToolCallToken struct {
	Type      enums.TokenType
	ID        string
	Index     int
	Name      string
	Arguments string
}

func (t *ToolCallToken) TokenType() enums.TokenType {
	return t.Type
}

func (t *ToolCallToken) Content() string {
	return t.Arguments
}

type StopToken struct {
	Type   enums.TokenType
	Reason string
}

func (t *StopToken) TokenType() enums.TokenType {
	return t.Type
}

func (t *StopToken) Content() string {
	return t.Reason
}

type UsageToken struct {
	Type         enums.TokenType
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func (t *UsageToken) TokenType() enums.TokenType {
	return t.Type
}

func (t *UsageToken) Content() string {
	return ""
}

type basicTokenStream struct {
	tokens <-chan Token
	cancel context.CancelFunc
}

func (s *basicTokenStream) Tokens() <-chan Token {
	return s.tokens
}

func (s *basicTokenStream) Cancel() {
	s.cancel()
}

func NewTokenStream(tokens <-chan Token, cancel context.CancelFunc) TokenStream {
	return &basicTokenStream{
		tokens: tokens,
		cancel: cancel,
	}
}
