package server

const (
	errCodeMethodNotAllowed       = "method_not_allowed"
	errCodeInvalidRequest         = "invalid_request"
	errCodeInvalidOptions         = "invalid_options"
	errCodePolicyError            = "policy_error"
	errCodePolicyRejected         = "policy_rejected"
	errCodeRouteError             = "route_error"
	errCodeExecutionFailed        = "execution_failed"
	errCodeGatewayTimeout         = "gateway_timeout"
	errCodeUnauthorized           = "unauthorized"
	errCodeOverload               = "overload"
	errCodeAgentNotImplemented    = "agent_not_implemented"
	errCodeRAGNotConfigured       = "rag_not_configured"
	errCodeUnsupportedRequestType = "unsupported_request_type"
)
