package app

import (
	"net/http"
	"time"

	"nautilus/internal/app/handlers"
	"nautilus/internal/app/handlers/admin"
	"nautilus/internal/app/handlers/apikeys"
	"nautilus/internal/app/handlers/auth"
	"nautilus/internal/app/handlers/notifications"
	"nautilus/internal/app/handlers/orgs"
	"nautilus/internal/app/handlers/users"
	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/featureflags"
	"nautilus/internal/database/postgres"
	"nautilus/internal/database/redis"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mail"
	"nautilus/internal/mail/ses"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/observability/tracer"
	"nautilus/internal/server"
)

type App struct {
	srv    *server.Server
	logger *log.Logger
}

type Config struct {
	Logger *log.Logger
}

func New(appconfig *Config) *App {
	srv := server.New(&server.Config{
		Logger: appconfig.Logger,
	})
	ctx := srv.Context()

	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		appconfig.Logger.Fatal("error connecting to database", "error", err)
	}
	srv.RegisterOnShutdown(database.Close(ctx, db))

	rdb, err := redis.Connect(ctx, config.Get[string]("REDIS_URL"))
	if err != nil {
		appconfig.Logger.Fatal("error connecting to redis", "error", err)
	}
	srv.RegisterOnShutdown(redis.Close(ctx, rdb))

	awsCfg, err := aws.LoadConfig(ctx)
	if err != nil {
		appconfig.Logger.Fatal("error loading AWS config", "error", err)
	}

	var sender mail.Sender
	if config.Get("APP_ENV", "development") == "development" {
		sender, err = mail.NewLocalSender()
		if err != nil {
			appconfig.Logger.Fatal("error creating local mail sender", "error", err)
		}
	} else {
		configSet := config.Get[string]("SES_CONFIGURATION_SET")
		sender = ses.NewSender(awsCfg, appconfig.Logger, configSet)
	}

	appTracer := newTracer(ctx, srv, appconfig.Logger)
	appStackTracer := newStackTracer(appTracer)

	r := mux.New(mux.Config{
		Middleware: []mux.Middleware{
			middleware.AccessLog,
			middleware.StackTrace(appStackTracer),
			middleware.Trace(appTracer),
			middleware.Recover,
			middleware.CORS(&middleware.CORSConfig{
				AllowOrigins:  config.Get("CORS_ALLOWED_ORIGINS", "*"),
				ExposeHeaders: "X-App-Version",
			}),
			middleware.AppVersion(config.Get[string]("APP_VERSION")),
		},
		NotFoundHandler:         handlers.NotFoundHandler,
		MethodNotAllowedHandler: handlers.MethodNotAllowedHandler,
	})

	r.HandleFunc(http.MethodGet, "/health", handlers.Healthcheck(ctx))

	ratelimiter := redis.NewLimiter(rdb, &redis.LimiterConfig{
		Capacity:     100,
		Interval:     time.Second,
		IdentityFunc: httputil.GetIP,
	})
	r.Use(middleware.Ratelimit(ratelimiter))

	r.HandleFunc(http.MethodGet, "/panic", handlers.Panic)

	counter := redis.NewCounter(ctx, rdb, &redis.CounterConfig{
		Interval: time.Minute,
	})
	flags := featureflags.FeatureFlagger(db)

	vapidPublicKey := config.Get[string]("VAPID_PUBLIC_KEY")
	tracedDB := tracer.NewTracedDatabase(db, appTracer)
	sender = tracer.NewTracedMailSender(sender, appTracer)

	authMux := auth.NewMux(ctx, tracedDB, sender, counter)
	userMux := users.NewMux(tracedDB, sender, flags)
	orgMux := orgs.NewMux(tracedDB)
	adminMux := admin.NewMux(tracedDB)
	apiKeyMux := apikeys.NewMux(tracedDB)
	notificationsMux := notifications.NewMux(tracedDB, vapidPublicKey)

	r.Get("/env", handlers.Env(authMux.SSOProviders()))
	authMux.Mount(r, "/auth")

	r.Use(middleware.RequireSession(db))
	r.Use(middleware.AdminOrgOverride(db))

	userMux.Mount(r, "/users")
	orgMux.Mount(r, "/orgs")
	adminMux.Mount(r, "/admin")
	apiKeyMux.Mount(r, "/api-keys")
	notificationsMux.Mount(r, "/notifications")

	srv.SetHandler(r)
	return &App{
		srv:    srv,
		logger: appconfig.Logger,
	}
}

func (app *App) Serve() {
	app.srv.Serve()
}
