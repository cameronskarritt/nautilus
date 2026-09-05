package enums

type Provider string
type Role string
type Model string
type TokenType string
type Verbosity string

const (
	ProviderNone      Provider = "none"
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderGemini    Provider = "gemini"

	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"

	TokenTypeError     TokenType = "error"
	TokenTypeText      TokenType = "text"
	TokenTypeToolCall  TokenType = "tool_call"
	TokenTypeStop      TokenType = "stop"
	TokenTypeReasoning TokenType = "reasoning"
	TokenTypeUsage     TokenType = "usage"

	VerbosityHigh   Verbosity = "high"
	VerbosityMedium Verbosity = "medium"
	VerbosityLow    Verbosity = "low"
)
