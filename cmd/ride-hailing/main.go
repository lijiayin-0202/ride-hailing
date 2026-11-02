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
	"github.com/lijayin-0202/radish-ride-service/internal/data"
)

// go build -ldflags "-X main.Version=x.y.z"
var (
	Name    = "ride-hailing"
	Version = "v1.0.0"
	// flagconf 配置文件目录，kratos 约定指向 configs 目录
	flagconf string
)

func init() {
	flag.StringVar(&flagconf, "conf", "../../configs", "config path, eg: -conf config.yaml")
}

func newApp(logger log.Logger, hs *http.Server, gs *grpc.Server, dispatcher *data.DispatchOutboxDispatcher, orderDispatcher *data.OrderDispatchOutboxDispatcher, timeoutWorker *data.DispatchTimeoutWorker, reassignmentDispatcher *data.DispatchReassignmentDispatcher) *kratos.App {
	return kratos.New(
		kratos.ID(serviceID()),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(hs, gs),
		// 所有后台工作者交给 Kratos 生命周期托管，保证停止时租约不再继续处理。
		kratos.AfterStart(dispatcher.Start),
		kratos.AfterStart(orderDispatcher.Start),
		kratos.AfterStart(timeoutWorker.Start),
		kratos.AfterStart(reassignmentDispatcher.Start),
		kratos.BeforeStop(dispatcher.Stop),
		kratos.BeforeStop(orderDispatcher.Stop),
		kratos.BeforeStop(timeoutWorker.Stop),
		kratos.BeforeStop(reassignmentDispatcher.Stop),
	)
}

func serviceID() string {
	// 实例名带主机名，便于分布式日志中区分多个调度服务副本。
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return Name
	}
	return Name + "-" + hostname
}

func main() {
	flag.Parse()

	logger := log.With(log.NewStdLogger(os.Stdout),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.id", serviceID(),
		"service.name", Name,
		"service.version", Version,
		"trace.id", tracing.TraceID(),
		"span.id", tracing.SpanID(),
	)

	c := config.New(
		config.WithSource(file.NewSource(flagconf)),
	)
	defer c.Close()

	// 配置缺失即拒绝启动，避免在未知数据库或跨模块地址下提供不安全的派单服务。
	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}

	// Wire 只负责装配依赖；数据库、后台工作者和服务端的关闭由 cleanup/App 生命周期兜底。
	app, cleanup, err := wireApp(bc.Server, bc.Data, logger)
	if err != nil {
		panic(err)
	}
	defer cleanup()

	if err := app.Run(); err != nil {
		panic(err)
	}
}
