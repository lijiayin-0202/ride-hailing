package biz

import (
	"errors"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
)

const (
	ErrorReasonInvalidInput      = "VEHICLE_HMI_INVALID_INPUT"
	ErrorReasonInvalidTransition = "VEHICLE_COMMAND_INVALID_TRANSITION"
	ErrorReasonInternal          = "VEHICLE_HMI_INTERNAL_ERROR"
)

// BusinessError 描述可转换为统一传输错误的业务错误。
type BusinessError struct {
	Code    int
	Reason  string
	Message string
}

// ToBusinessError 将内部错误映射为稳定的业务错误码和提示。
func ToBusinessError(err error) BusinessError {
	switch {
	case errors.Is(err, ErrInvalidVehicleInput):
		return BusinessError{Code: 400, Reason: ErrorReasonInvalidInput, Message: "invalid vehicle hmi input"}
	case errors.Is(err, ErrInvalidCommandTransition):
		return BusinessError{Code: 409, Reason: ErrorReasonInvalidTransition, Message: "invalid vehicle command transition"}
	default:
		return BusinessError{Code: 500, Reason: ErrorReasonInternal, Message: "vehicle hmi internal error"}
	}
}

// ToTransportError 将业务错误转换为 Kratos 统一错误响应。
func ToTransportError(item BusinessError) error {
	return kratoserrors.New(item.Code, item.Reason, item.Message)
}
