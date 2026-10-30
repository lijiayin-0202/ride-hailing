// 模块1 用户账号与个人中心（成员A）
// 统一 HTTP 返回封装：{ code, message, requestId, data }（前端页面接口文档 §1.1）。
//
// 说明：公共契约由模块6牵头冻结；此处先提供本模块可用的实现，
// 待模块6统一契约落地后，直接替换本文件实现即可，业务代码无需改动。
package server

import (
	"bytes"
	"encoding/json"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

const (
	// successCode 成功业务码，与前端接口文档示例保持一致。
	successCode = "0"
	// successMsg 成功提示文案，前端可忽略。
	successMsg = "ok"
	// requestIDHeader 链路追踪头：请求可传入，缺省时服务端生成并回写。
	requestIDHeader = "X-Request-Id"
)

// envelope 统一响应体。data 失败时为 null。
type envelope struct {
	// Code 业务码："0" 成功，其余见 pkg/errcode。
	Code string `json:"code"`
	// Message 面向用户的提示，可直接展示。
	Message string `json:"message"`
	// RequestID 链路追踪 ID，排查问题用。
	RequestID string `json:"requestId"`
	// Data 业务数据；失败时固定为 null。
	Data json.RawMessage `json:"data"`
}

// knownErrorCodes 只允许文档已定义的错误码对外输出。
var knownErrorCodes = map[string]struct{}{
	biz.CodeInvalidArgument:  {},
	biz.CodeUnauthorized:     {},
	biz.CodeForbidden:        {},
	biz.CodeResourceNotFound: {},
	biz.CodeBusinessConflict: {},
	biz.CodeRateLimited:      {},
	biz.CodeInternalError:    {},
}

// ResponseEncoder 成功响应编码器。
func ResponseEncoder(w http.ResponseWriter, r *http.Request, v any) error {
	// 沿用调用链传入的 requestId，缺省时生成。
	requestID := ensureRequestID(r)

	// 统一 JSON + 请求 ID 响应头。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(requestIDHeader, requestID)

	// 业务数据序列化失败按内部错误处理（由上层 ErrorEncoder 统一出口）。
	data, err := marshalData(v)
	if err != nil {
		return err
	}
	// 成功统一 200，业务结果由 code 字段表达。
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(envelope{
		Code:      successCode,
		Message:   successMsg,
		RequestID: requestID,
		Data:      data,
	})
}

// ErrorEncoder 错误响应编码器：HTTP 状态码取自 kratos error，业务码对齐文档枚举。
func ErrorEncoder(w http.ResponseWriter, r *http.Request, err error) {
	requestID := ensureRequestID(r)
	// 把任意 error 归一成 kratos error，取 HTTP 状态与业务码。
	se := kerrors.FromError(err)

	status := int(se.Code)
	// 非法状态码兜底为 500，避免写不出响应头。
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	code := errorCode(se)
	message := se.Message
	if code == biz.CodeInternalError {
		// 内部错误不向客户端暴露实现细节（AGENT.md §10.1）。
		message = "服务内部错误，请稍后重试"
	}
	// 业务码有值但 message 为空时，用状态码文案兜底。
	if message == "" {
		message = http.StatusText(status)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(requestIDHeader, requestID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{
		Code:      code,
		Message:   message,
		RequestID: requestID,
		Data:      json.RawMessage("null"),
	})
}

// errorCode 把任意 error 映射成文档定义的业务码。
//
// 优先使用 kratos error 的 reason（业务层显式指定）；
// 若 reason 不在文档枚举内（例如框架的 CODEC 请求解析失败、RECOVERY 兜底），
// 则按 HTTP 状态码归类到最接近的公共码，避免把「参数格式错」误报成「服务内部错误」。
func errorCode(se *kerrors.Error) string {
	if _, ok := knownErrorCodes[se.Reason]; ok {
		return se.Reason
	}
	switch int(se.Code) {
	case http.StatusBadRequest:
		return biz.CodeInvalidArgument
	case http.StatusUnauthorized:
		return biz.CodeUnauthorized
	case http.StatusForbidden:
		return biz.CodeForbidden
	case http.StatusNotFound:
		return biz.CodeResourceNotFound
	case http.StatusConflict:
		return biz.CodeBusinessConflict
	case http.StatusTooManyRequests:
		return biz.CodeRateLimited
	default:
		return biz.CodeInternalError
	}
}

// marshalData 序列化业务数据。
//
// 与 protojson 默认行为的差异（都是为了对齐前端接口文档示例）：
//  1. 字段名使用 proto 原始 snake_case；
//  2. 输出未赋值的字段（集合输出 []、数值输出 0），避免前端取不到 items/total；
//  3. int64/uint64 输出为 JSON 数值而非字符串（proto3 JSON 规范默认是字符串）。
func marshalData(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage("null"), nil
	}
	msg, ok := v.(proto.Message)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(b), nil
	}

	raw, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return normalizeNumbers(msg.ProtoReflect().Descriptor(), raw)
}

// normalizeNumbers 依据 message 描述符把 int64/uint64 的字符串还原成 JSON 数值。
func normalizeNumbers(desc protoreflect.MessageDescriptor, raw []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	fieldMap, ok := decoded.(map[string]any)
	if !ok {
		return json.RawMessage(raw), nil
	}
	b, err := json.Marshal(convertMessageMap(desc, fieldMap))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func convertMessageMap(desc protoreflect.MessageDescriptor, value map[string]any) map[string]any {
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		key := string(fd.Name())
		raw, ok := value[key]
		if !ok || raw == nil {
			continue
		}
		value[key] = convertFieldValue(fd, raw)
	}
	return value
}

func convertFieldValue(fd protoreflect.FieldDescriptor, raw any) any {
	if fd.IsList() {
		items, ok := raw.([]any)
		if !ok {
			return raw
		}
		for i := range items {
			items[i] = convertSingularValue(fd, items[i])
		}
		return items
	}
	if fd.IsMap() {
		entries, ok := raw.(map[string]any)
		if !ok {
			return raw
		}
		for k, item := range entries {
			entries[k] = convertSingularValue(fd.MapValue(), item)
		}
		return entries
	}
	return convertSingularValue(fd, raw)
}

func convertSingularValue(fd protoreflect.FieldDescriptor, raw any) any {
	if raw == nil {
		return nil
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if sub, ok := raw.(map[string]any); ok {
			return convertMessageMap(fd.Message(), sub)
		}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if text, ok := raw.(string); ok {
			return json.Number(text)
		}
	}
	return raw
}

// ensureRequestID 沿用调用链传入的 X-Request-Id，缺省时生成。
func ensureRequestID(r *http.Request) string {
	if r == nil {
		return "req_" + uuid.NewString()
	}
	if id := r.Header.Get(requestIDHeader); id != "" {
		return id
	}
	return "req_" + uuid.NewString()
}
