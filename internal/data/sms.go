// 模块1 用户账号与个人中心（成员A）
// 登录验证码的短期存储与短信通道实现。
//
// 设计要点：
//  1. 验证码只保存 sha256 哈希（biz.SmsChallenge.CodeHash），任何存储都不落明文；
//  2. 首选 Redis（项目固定缓存选型，AGENT.md §2），Redis 不可用时退化为进程内存储，
//     仅适合单实例开发/联调，启动时打印告警；
//  3. 短信通道未接入真实供应商前使用日志实现，默认不打印验证码；
//     本地联调可显式设置 RIDE_SMS_ECHO_CODE=1 让验证码输出到日志（生产必须关闭）。
package data

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"

	"github.com/lijayin-0202/radish-ride-service/internal/biz"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
	"github.com/lijayin-0202/radish-ride-service/pkg/mask"
)

const (
	// smsChallengeKeyPrefix 挑战明细键：按 verification_id 存哈希与过期时间。
	smsChallengeKeyPrefix = "ride:sms:challenge:"
	// smsMobileIndexPrefix 手机号索引键：一个手机号只保留最近一次挑战。
	smsMobileIndexPrefix = "ride:sms:mobile:"
	// smsAttemptKeyPrefix 错误次数计数键，配合 5 次上限使用。
	smsAttemptKeyPrefix = "ride:sms:attempt:"
	// smsResendKeyPrefix 发送冷却锁键，SETNX 实现 60 秒限流。
	smsResendKeyPrefix = "ride:sms:resend:"
)

// resendKey 发送冷却锁的键：按「场景 + 手机号」隔离，避免不同业务互相阻塞。
func resendKey(scene, mobile string) string {
	return smsResendKeyPrefix + scene + ":" + mobile
}

// NewSmsCodeStore 验证码存储：优先 Redis，不可用时退化为进程内存储。
func NewSmsCodeStore(d *Data, logger log.Logger) biz.SmsCodeStore {
	helper := log.NewHelper(log.With(logger, "module", "data/sms_store"))
	if d.rdb != nil {
		return &redisSmsCodeStore{rdb: d.rdb, log: helper}
	}
	helper.Warn("Redis 未连接，验证码使用进程内存储：仅限单实例开发/联调，生产必须启用 Redis")
	return newMemorySmsCodeStore()
}

// ---------------------------- Redis 实现 ----------------------------

// redisSmsCodeStore Redis 版验证码存储（多实例共用，生产唯一推荐实现）。
type redisSmsCodeStore struct {
	// rdb Redis 客户端，非 nil 时由 NewSmsCodeStore 选用。
	rdb *redis.Client
	// log 结构化日志。
	log *log.Helper
}

// SaveChallenge 保存挑战：明细键 + 手机号索引键同时写入，TTL 与验证码有效期一致。
func (s *redisSmsCodeStore) SaveChallenge(ctx context.Context, challenge *biz.SmsChallenge, ttl time.Duration) error {
	// 存的是含哈希的结构体，不含验证码明文。
	payload, err := json.Marshal(challenge)
	if err != nil {
		return err
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, smsChallengeKeyPrefix+challenge.VerificationID, payload, ttl)
	pipe.Set(ctx, smsMobileIndexPrefix+challenge.Mobile, challenge.VerificationID, ttl)
	// 手机号维度只保留最近一次挑战，避免同号多挑战被重放。
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return nil
}

// GetChallenge 按挑战编号读取；不存在返回 (nil, nil)。
func (s *redisSmsCodeStore) GetChallenge(ctx context.Context, verificationID string) (*biz.SmsChallenge, error) {
	// 空编号按"未提供"处理，由调用方回退到手机号索引查询。
	if verificationID == "" {
		return nil, nil
	}
	raw, err := s.rdb.Get(ctx, smsChallengeKeyPrefix+verificationID).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var challenge biz.SmsChallenge
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return nil, err
	}
	if attempts, err := s.rdb.Get(ctx, smsAttemptKeyPrefix+verificationID).Int(); err == nil {
		challenge.Attempts = int32(attempts)
	}
	return &challenge, nil
}

// GetChallengeByMobile 按手机号取最近一次挑战（兼容前端只传手机号的场景）。
func (s *redisSmsCodeStore) GetChallengeByMobile(ctx context.Context, mobile string) (*biz.SmsChallenge, error) {
	// 先查索引键拿到 verification_id，再复用按编号查询的逻辑。
	verificationID, err := s.rdb.Get(ctx, smsMobileIndexPrefix+mobile).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return s.GetChallenge(ctx, verificationID)
}

// IncrAttempt 累加错误次数并返回最新值（同时续期，避免计数键先于挑战过期）。
func (s *redisSmsCodeStore) IncrAttempt(ctx context.Context, verificationID string) (int32, error) {
	key := smsAttemptKeyPrefix + verificationID
	pipe := s.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, biz.SmsCodeTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return int32(incr.Val()), nil
}

// DeleteChallenge 一次性消费：明细、计数、手机号索引一并删除，防止重放。
func (s *redisSmsCodeStore) DeleteChallenge(ctx context.Context, verificationID string) error {
	// 先读出挑战，才能顺带清理其手机号索引键。
	challenge, err := s.GetChallenge(ctx, verificationID)
	if err != nil {
		return err
	}
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, smsChallengeKeyPrefix+verificationID)
	pipe.Del(ctx, smsAttemptKeyPrefix+verificationID)
	if challenge != nil && challenge.Mobile != "" {
		pipe.Del(ctx, smsMobileIndexPrefix+challenge.Mobile)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// AcquireResendLock 获取发送冷却锁：SETNX 成功表示本场景该手机号首次发送。
// key 含场景，避免「刚登录」把紧接着的换绑原号验证也一起限流。
func (s *redisSmsCodeStore) AcquireResendLock(ctx context.Context, scene, mobile string, ttl time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, resendKey(scene, mobile), "1", ttl).Result()
}

// ReleaseResendLock 释放冷却锁（短信通道失败时回滚），用户可立即重发。
func (s *redisSmsCodeStore) ReleaseResendLock(ctx context.Context, scene, mobile string) error {
	return s.rdb.Del(ctx, resendKey(scene, mobile)).Err()
}

// compareAndDeleteScript 原子「比对哈希 + 删除挑战」。
// 必要性：GET 与 DEL 分两步会留出并发窗口，同一个验证码可被并发请求签发两个令牌。
// KEYS[1]=挑战键 KEYS[2]=错误计数键；ARGV[1]=待比对哈希 ARGV[2]=手机号索引前缀。
var compareAndDeleteScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return 0 end
local ok, obj = pcall(cjson.decode, raw)
if not ok or obj['code_hash'] ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
redis.call('DEL', KEYS[2])
if obj['mobile'] and obj['mobile'] ~= '' then
  redis.call('DEL', ARGV[2] .. obj['mobile'])
end
return 1
`)

// CompareAndDelete 原子校验并消费挑战，返回 true 表示本次消费成功。
func (s *redisSmsCodeStore) CompareAndDelete(ctx context.Context, verificationID, codeHash string) (bool, error) {
	result, err := compareAndDeleteScript.Run(ctx, s.rdb,
		[]string{smsChallengeKeyPrefix + verificationID, smsAttemptKeyPrefix + verificationID},
		codeHash, smsMobileIndexPrefix).Int()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	return result == 1, nil
}

// ---------------------------- 进程内实现（Redis 不可用兜底） ----------------------------

// memorySmsCodeStore 进程内验证码存储（Redis 不可用时的兜底，仅限单实例联调）。
type memorySmsCodeStore struct {
	// mu 保护下列 map，所有读写方法都要加锁。
	mu sync.Mutex
	// challenges verification_id → 挑战明细。
	challenges map[string]*biz.SmsChallenge
	// mobileIndex 手机号 → 最近一次 verification_id。
	mobileIndex map[string]string
	// attempts verification_id → 错误次数。
	attempts map[string]int32
	// resendUntil 手机号 → 冷却截止时间。
	resendUntil map[string]time.Time
}

// newMemorySmsCodeStore 初始化进程内存储的全部索引表。
func newMemorySmsCodeStore() *memorySmsCodeStore {
	return &memorySmsCodeStore{
		challenges:  make(map[string]*biz.SmsChallenge),
		mobileIndex: make(map[string]string),
		attempts:    make(map[string]int32),
		resendUntil: make(map[string]time.Time),
	}
}

// SaveChallenge 保存挑战（写入前顺带清理过期数据，避免 map 无界增长）。
func (s *memorySmsCodeStore) SaveChallenge(ctx context.Context, challenge *biz.SmsChallenge, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(time.Now())
	copied := *challenge
	s.challenges[challenge.VerificationID] = &copied
	s.mobileIndex[challenge.Mobile] = challenge.VerificationID
	return nil
}

// GetChallenge 按编号读取挑战（返回副本，避免调用方改动内部状态）。
func (s *memorySmsCodeStore) GetChallenge(ctx context.Context, verificationID string) (*biz.SmsChallenge, error) {
	// 空编号视为未提供。
	if verificationID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challenges[verificationID]
	if !ok {
		return nil, nil
	}
	copied := *challenge
	copied.Attempts = s.attempts[verificationID]
	return &copied, nil
}

// GetChallengeByMobile 按手机号索引取最近一次挑战。
func (s *memorySmsCodeStore) GetChallengeByMobile(ctx context.Context, mobile string) (*biz.SmsChallenge, error) {
	// 只在锁内取索引，随后复用按编号查询（避免长时间持锁）。
	s.mu.Lock()
	verificationID, ok := s.mobileIndex[mobile]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	return s.GetChallenge(ctx, verificationID)
}

// IncrAttempt 累加错误次数并返回最新值。
func (s *memorySmsCodeStore) IncrAttempt(ctx context.Context, verificationID string) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[verificationID]++
	return s.attempts[verificationID], nil
}

// DeleteChallenge 删除挑战及其手机号索引、错误计数。
func (s *memorySmsCodeStore) DeleteChallenge(ctx context.Context, verificationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if challenge, ok := s.challenges[verificationID]; ok {
		delete(s.mobileIndex, challenge.Mobile)
	}
	delete(s.challenges, verificationID)
	delete(s.attempts, verificationID)
	return nil
}

// AcquireResendLock 获取发送冷却锁：冷却期内返回 false（按场景 + 手机号隔离）。
func (s *memorySmsCodeStore) AcquireResendLock(ctx context.Context, scene, mobile string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resendKey(scene, mobile)
	now := time.Now()
	if until, ok := s.resendUntil[key]; ok && now.Before(until) {
		return false, nil
	}
	s.resendUntil[key] = now.Add(ttl)
	return true, nil
}

// ReleaseResendLock 释放冷却锁（短信通道失败时回滚）。
func (s *memorySmsCodeStore) ReleaseResendLock(ctx context.Context, scene, mobile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.resendUntil, resendKey(scene, mobile))
	return nil
}

// CompareAndDelete 在锁内完成「比对 + 删除」，等价于 Redis 的原子消费脚本。
func (s *memorySmsCodeStore) CompareAndDelete(ctx context.Context, verificationID, codeHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challenges[verificationID]
	if !ok || challenge.CodeHash != codeHash {
		return false, nil
	}
	delete(s.mobileIndex, challenge.Mobile)
	delete(s.challenges, verificationID)
	delete(s.attempts, verificationID)
	return true, nil
}

// purgeExpiredLocked 清理过期挑战与过期冷却锁；调用方必须已持有 s.mu。
func (s *memorySmsCodeStore) purgeExpiredLocked(now time.Time) {
	// 挑战过期后连同手机号索引与错误计数一起清理。
	for id, challenge := range s.challenges {
		if now.After(challenge.ExpiresAt) {
			delete(s.mobileIndex, challenge.Mobile)
			delete(s.challenges, id)
			delete(s.attempts, id)
		}
	}
	for mobile, until := range s.resendUntil {
		if now.After(until) {
			delete(s.resendUntil, mobile)
		}
	}
}

// ---------------------------- 短信通道 ----------------------------

// smsSender 短信通道实现（当前只记录发送事件，未接真实供应商）。
type smsSender struct {
	// log 结构化日志。
	log *log.Helper
	// echoCode 联调开关：true 时把验证码打进日志（生产必须关闭）。
	echoCode bool
}

// NewSmsSender 短信通道实现。
// 真实供应商（阿里云/腾讯云短信）接入点在此处替换；当前实现只记录发送事件。
func NewSmsSender(logger log.Logger) biz.SmsSender {
	helper := log.NewHelper(log.With(logger, "module", "data/sms_sender"))
	// 开关读取收敛到 internal/conf（.agent/04 §2.1）。
	echo := conf.SmsEchoCodeEnabled()
	if echo {
		helper.Warn("RIDE_SMS_ECHO_CODE 已开启：验证码将写入日志，仅限本地联调，生产必须关闭")
	}
	return &smsSender{log: helper, echoCode: echo}
}

func (s *smsSender) SendLoginCode(ctx context.Context, mobile, code string) error {
	if s.echoCode {
		// 仅在显式开启联调开关时输出，便于本地用真实流程走通登录。
		s.log.WithContext(ctx).Warnf("[联调] 登录验证码 mobile=%s code=%s", mask.Mobile(mobile), code)
		return nil
	}
	s.log.WithContext(ctx).Infof("短信验证码已提交发送通道 mobile=%s code_length=%d", mask.Mobile(mobile), len(code))
	return nil
}

// 编译期断言
var (
	_ biz.SmsCodeStore = (*redisSmsCodeStore)(nil)
	_ biz.SmsCodeStore = (*memorySmsCodeStore)(nil)
	_ biz.SmsSender    = (*smsSender)(nil)
)
