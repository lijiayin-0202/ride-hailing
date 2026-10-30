// 服务入口：解析 -conf → 读取配置 → wire 组装 kratos App → 启动 → 优雅退出。
//
// 分层约定（AGENT.md §7）：本文件只做进程级编排，禁止写业务逻辑；
// 依赖装配全部交给 wire_gen.go（由 `make generate` 生成，禁止手改）。
package main

import (
	"flag"
	"os"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/go-kratos/kratos/v2/transport/http"

	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

// go build -ldflags "-X main.Version=x.y.z"
var (
	// Name 服务名，写入日志与注册中心。
	Name = "ride-hailing"
	// Version 构建版本，可由 ldflags 覆盖。
	Version = "v1.0.0"
	// flagconf 配置文件目录，kratos 约定指向 configs 目录
	flagconf string
)

// init 注册命令行参数：-conf 指定配置目录（本地默认 ../../configs）。
func init() {
	flag.StringVar(&flagconf, "conf", "../../configs", "config path, eg: -conf config.yaml")
}

// newApp 组装 kratos 应用：ID/名称/版本/日志/传输层服务。
func newApp(logger log.Logger, hs *http.Server, gs *grpc.Server) *kratos.App {
	return kratos.New(
		kratos.ID(serviceID()),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		// HTTP 与 gRPC 双协议同时注册（AGENT.md §2）。
		kratos.Server(hs, gs),
	)
}

// serviceID 实例标识：服务名 + 主机名，便于多实例排障；取不到主机名时退化为服务名。
func serviceID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return Name
	}
	return Name + "-" + hostname
}

// main 进程入口。
func main() {
	// ① 解析命令行参数（-conf）
	flag.Parse()

	// ② 构造结构化日志：附带服务标识与 trace/span，便于日志聚合检索。
	logger := log.With(log.NewStdLogger(os.Stdout),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.id", serviceID(),
		"service.name", Name,
		"service.version", Version,
		"trace.id", tracing.TraceID(),
		"span.id", tracing.SpanID(),
	)

	// ③ 加载配置文件（仅 yaml；敏感项由 internal/conf 从环境变量补齐）
	c := config.New(
		config.WithSource(file.NewSource(flagconf)),
	)
	defer c.Close()

	// 加载失败直接退出，避免带病启动。
	if err := c.Load(); err != nil {
		panic(err)
	}

	// ④ 反序列化到 Bootstrap（Server/Data 等结构）
	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}

	// ⑤ wire 组装依赖（repo → usecase → service → server）
	app, cleanup, err := wireApp(bc.Server, bc.Data, logger)
	if err != nil {
		panic(err)
	}
	// 进程退出时释放连接池等资源。
	defer cleanup()

	// ⑥ 启动并阻塞；监听 :8000(HTTP) / :9000(gRPC)
	if err := app.Run(); err != nil {
		panic(err)
	}
}
