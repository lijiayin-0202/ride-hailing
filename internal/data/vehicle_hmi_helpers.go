package data

import (
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
)

// scanCommands 扫描车辆指令列表并统一关闭结果集。
func scanCommands(rows pgx.Rows) ([]*biz.VehicleCommand, error) {
	defer rows.Close()
	commands := make([]*biz.VehicleCommand, 0)
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}

// scanCommand 将 PostgreSQL 查询结果转换为业务指令实体。
func scanCommand(row pgx.Row) (*biz.VehicleCommand, error) {
	command := &biz.VehicleCommand{}
	var status int32
	if err := row.Scan(&command.CommandNo, &command.VehicleID, &command.RelatedOrderID, &command.CommandType, &command.CommandPayload, &command.Priority, &status, &command.IssuedAt, &command.AcknowledgedAt, &command.FinishedAt, &command.ResultMessage); err != nil {
		return nil, err
	}
	command.Status = biz.CommandStatus(status)
	return command, nil
}

// defaultJSON 保证写入 JSONB 字段的数据始终是合法 JSON。
func defaultJSON(data []byte) []byte {
	if len(data) == 0 || !json.Valid(data) {
		return []byte(`{}`)
	}
	return data
}
