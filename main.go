package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/cluster"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/i18n"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/modelrouter"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/router"
)

//go:embed all:web/build
var buildFS embed.FS

func main() {
	common.Init()
	if *common.DaemonMode {
		dm := common.NewDaemonManager(*common.PidFile, *common.DataDir)
		if os.Getenv("ONE_API_PRO_DAEMON") != "1" {
			if err := dm.EnsureCanStart(); err != nil {
				logger.FatalLog(err.Error())
			}
		}
		common.SetupDaemonSignals(dm, func() {
			logger.SysLog("daemon shutting down...")
		})
		if err := common.Daemonize(common.GetDaemonLogFilePath()); err != nil {
			logger.FatalLog("failed to daemonize: " + err.Error())
		}
		if err := dm.WritePidFile(); err != nil {
			logger.FatalLog("failed to write PID file: " + err.Error())
		}
	}
	logger.SetupLogger()
	logger.SysLogf("One Api Pro %s started", common.Version)

	if os.Getenv("GIN_MODE") != gin.DebugMode {
		gin.SetMode(gin.ReleaseMode)
	}
	if config.DebugEnabled {
		logger.SysLog("running in debug mode")
	}

	// Initialize SQL Database
	model.InitDB()
	model.InitLogDB()

	var err error
	err = model.CreateRootAccountIfNeed()
	if err != nil {
		logger.FatalLog("database init error: " + err.Error())
	}
	defer func() {
		err := model.CloseDB()
		if err != nil {
			logger.FatalLog("failed to close database: " + err.Error())
		}
	}()

	// Initialize Redis
	err = common.InitRedisClient()
	if err != nil {
		logger.FatalLog("failed to initialize Redis: " + err.Error())
	}

	// Initialize options
	model.InitOptionMap()
	// 对账 abilities：历史导入/直接改库的渠道不会生成 abilities 记录，
	// 导致令牌页可用模型、/v1/models、model auto 路由取不到模型。
	// 启动时从 channels 补齐缺失记录（只补不删，幂等）。失败不阻断启动。
	if _, err := model.ReconcileAbilitiesFromChannels(); err != nil {
		logger.SysError("failed to reconcile abilities from channels: " + err.Error())
	}
	logger.SysLog(fmt.Sprintf("using theme %s", config.Theme))
	if err := common.ValidateEmbeddedTheme(buildFS, "web/build", config.Theme); err != nil {
		logger.SysError(err.Error())
	}
	if common.RedisEnabled {
		// for compatibility with old versions
		config.MemoryCacheEnabled = true
	}
	if config.MemoryCacheEnabled {
		logger.SysLog("memory cache enabled")
		logger.SysLog(fmt.Sprintf("sync frequency: %d seconds", config.SyncFrequency))
		model.InitChannelCache()
	}
	if config.MemoryCacheEnabled {
		go model.SyncOptions(config.SyncFrequency)
		go model.SyncChannelCache(config.SyncFrequency)
	}
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err != nil {
			logger.FatalLog("failed to parse CHANNEL_TEST_FREQUENCY: " + err.Error())
		}
		go controller.AutomaticallyTestChannels(frequency)
	}
	if os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		config.BatchUpdateEnabled = true
		logger.SysLog("batch update enabled with interval " + strconv.Itoa(config.BatchUpdateInterval) + "s")
		model.InitBatchUpdater()
	}
	if config.EnableMetric {
		logger.SysLog("metric enabled, will disable channel if too much request failed")
	}
	openai.InitTokenEncoders()
	client.Init()
	channelrouter.InitRouter()
	modelrouter.InitRouter()
	if prewarmed, err := modelrouter.PrewarmDefaultRouter(context.Background()); err != nil {
		logger.FatalLog("failed to initialize cluster router: " + err.Error())
	} else if prewarmed {
		logger.SysLog("cluster router is ready")
	}

	// 初始化集群模块
	cluster.Init(model.DB)

	// Initialize i18n
	if err := i18n.Init(); err != nil {
		logger.FatalLog("failed to initialize i18n: " + err.Error())
	}

	// Initialize HTTP server
	server := gin.New()
	server.Use(gin.Recovery())
	// This will cause SSE not to work!!!
	//server.Use(gzip.Gzip(gzip.DefaultCompression))
	server.Use(middleware.RequestId())
	server.Use(middleware.Language())
	middleware.SetUpLogger(server)
	// Initialize session store
	store := cookie.NewStore([]byte(config.SessionSecret))
	server.Use(sessions.Sessions("session", store))

	router.SetRouter(server, buildFS)
	listenAddr := *common.Addr
	if listenAddr == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = strconv.Itoa(*common.Port)
		}
		listenAddr = ":" + port
	}
	logger.SysLogf("server started on %s", listenAddr)

	err = server.Run(listenAddr)
	if err != nil {
		logger.FatalLog("failed to start HTTP server: " + err.Error())
	}
}
