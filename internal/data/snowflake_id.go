package data

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	// 10 位节点号支持 1024 个实例；12 位序列号支持同一毫秒最多 4096 个 ID。
	snowflakeNodeBits                          = 10
	snowflakeSequenceBits                      = 12
	snowflakeMaxNodeID                         = (1 << snowflakeNodeBits) - 1
	snowflakeSequenceMask                      = (1 << snowflakeSequenceBits) - 1
	dispatchSnowflakeNodeIDEnvironmentVariable = "DISPATCH_SNOWFLAKE_NODE_ID"
)

var snowflakeEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// SnowflakeDispatchIDGenerator 生成可写入 PostgreSQL BIGINT 主键的应用侧全局 ID。
type SnowflakeDispatchIDGenerator struct {
	mu               sync.Mutex
	nodeID           int64
	lastMilliseconds int64
	sequence         int64
}

// NewSnowflakeDispatchIDGenerator 创建指定节点的派单记录 ID 生成器。
func NewSnowflakeDispatchIDGenerator(nodeID int64) (*SnowflakeDispatchIDGenerator, error) {
	if nodeID < 0 || nodeID > snowflakeMaxNodeID {
		return nil, fmt.Errorf("snowflake node ID must be between 0 and %d", snowflakeMaxNodeID)
	}

	return &SnowflakeDispatchIDGenerator{nodeID: nodeID}, nil
}

// NewDispatchIDGenerator 从部署环境读取节点标识，缺失或无效时拒绝启动。
func NewDispatchIDGenerator() (*SnowflakeDispatchIDGenerator, error) {
	// 禁止缺省节点号启动，宁可失败也不能让多个实例静默使用相同 ID 空间。
	nodeID, err := dispatchSnowflakeNodeIDFromEnvironment(os.LookupEnv)
	if err != nil {
		return nil, err
	}
	return NewSnowflakeDispatchIDGenerator(nodeID)
}

func dispatchSnowflakeNodeIDFromEnvironment(lookupEnv func(string) (string, bool)) (int64, error) {
	value, present := lookupEnv(dispatchSnowflakeNodeIDEnvironmentVariable)
	if !present || value == "" {
		return 0, fmt.Errorf("%s is required", dispatchSnowflakeNodeIDEnvironmentVariable)
	}

	nodeID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", dispatchSnowflakeNodeIDEnvironmentVariable, err)
	}
	if nodeID < 0 || nodeID > snowflakeMaxNodeID {
		return 0, fmt.Errorf("%s must be between 0 and %d", dispatchSnowflakeNodeIDEnvironmentVariable, snowflakeMaxNodeID)
	}
	return nodeID, nil
}

// New 返回一个单进程内递增的雪花 ID。
func (generator *SnowflakeDispatchIDGenerator) New() int64 {
	generator.mu.Lock()
	defer generator.mu.Unlock()

	now := time.Since(snowflakeEpoch).Milliseconds()
	// 系统时钟回拨时沿用最后毫秒，优先保持 ID 单调性和唯一性。
	if now < generator.lastMilliseconds {
		now = generator.lastMilliseconds
	}
	if now == generator.lastMilliseconds {
		generator.sequence = (generator.sequence + 1) & snowflakeSequenceMask
		if generator.sequence == 0 {
			now = generator.waitNextMillisecond(now)
		}
	} else {
		generator.sequence = 0
	}

	generator.lastMilliseconds = now
	return (now << (snowflakeNodeBits + snowflakeSequenceBits)) |
		(generator.nodeID << snowflakeSequenceBits) | generator.sequence
}

func (generator *SnowflakeDispatchIDGenerator) waitNextMillisecond(lastMilliseconds int64) int64 {
	for now := time.Since(snowflakeEpoch).Milliseconds(); now <= lastMilliseconds; now = time.Since(snowflakeEpoch).Milliseconds() {
		time.Sleep(time.Millisecond)
	}
	return time.Since(snowflakeEpoch).Milliseconds()
}
