package data

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// isOptionalRelationError 识别跨模块只读聚合时可以忽略的错误：
// 表/字段尚未由对应模块交付时（42P01 / 42703），聚合降级为空而不是整单失败。
func isOptionalRelationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "42P01") ||
		strings.Contains(message, "42703") ||
		strings.Contains(message, "does not exist")
}

// queryRowMaps 以 SELECT * 方式读取行并映射为 map，避免依赖跨模块表的列名假设。
func (r *rideRepo) queryRowMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := r.data.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, 4)
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for index, column := range columns {
			item[column] = values[index]
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// firstRow 读取单行，表/列缺失时返回 nil 且不报错。
func (r *rideRepo) firstRow(ctx context.Context, query string, args ...any) map[string]any {
	rows, err := r.queryRowMaps(ctx, query, args...)
	if err != nil {
		if isOptionalRelationError(err) {
			return nil
		}
		r.data.log.WithContext(ctx).Warnf("optional query failed: %v", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (r *rideRepo) allRows(ctx context.Context, query string, args ...any) []map[string]any {
	rows, err := r.queryRowMaps(ctx, query, args...)
	if err != nil {
		if isOptionalRelationError(err) {
			return nil
		}
		r.data.log.WithContext(ctx).Warnf("optional query failed: %v", err)
		return nil
	}
	return rows
}

func mapString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case []byte:
			return string(typed)
		case int64:
			return strconv.FormatInt(typed, 10)
		case int32:
			return strconv.FormatInt(int64(typed), 10)
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		}
	}
	return ""
}

func mapInt64(row map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case int64:
			return typed
		case int32:
			return int64(typed)
		case int16:
			return int64(typed)
		case float64:
			return int64(typed)
		case []byte:
			if parsed, err := strconv.ParseInt(string(typed), 10, 64); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func mapFloat(row map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed
		case float32:
			return float64(typed)
		case int64:
			return float64(typed)
		case []byte:
			if parsed, err := strconv.ParseFloat(string(typed), 64); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseFloat(typed, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func mapCents(row map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		var text string
		switch typed := value.(type) {
		case []byte:
			text = string(typed)
		case string:
			text = typed
		default:
			return int64(mapFloat(row, key) * 100) // 兼容 float 列
		}
		var amount cents
		if err := amount.Scan(text); err == nil {
			return int64(amount)
		}
	}
	return 0
}

func mapBytes(row map[string]any, keys ...string) []byte {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []byte:
			return typed
		case string:
			return []byte(typed)
		}
	}
	return nil
}

func mapTime(row map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case time.Time:
			return &typed
		case []byte:
			if parsed, err := time.Parse(time.RFC3339Nano, string(typed)); err == nil {
				return &parsed
			}
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				return &parsed
			}
		}
	}
	return nil
}
