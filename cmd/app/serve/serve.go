package serve

import (
	"nautilus/internal/app"
	"nautilus/internal/config"
	"nautilus/internal/log"
)

func Run() {
	config.LoadDotenv()

	appConfig := &app.Config{
		Logger: log.InferLogger("app"),
	}

	app.New(appConfig).Serve()
}
