package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/jinzhu/gorm"
	"github.com/yamnikov-oleg/avamon-bot/frontend/telegrambot"
	"github.com/yamnikov-oleg/avamon-bot/monitor"
)

func monitorCreate(b *telegrambot.Bot, config *Config) error {
	mon := monitor.New(b.DB)
	mon.Scheduler.Interval = time.Duration(config.Monitor.Interval) * time.Second
	mon.Scheduler.ParallelPolls = config.Monitor.MaxParallel
	mon.Scheduler.Poller.Timeout = time.Duration(config.Monitor.Timeout) * time.Second
	mon.NotifyFirstOK = config.Monitor.NotifyFirstOK

	ropts := monitor.RedisOptions{
		Host:     config.Redis.Host,
		Port:     config.Redis.Port,
		Password: config.Redis.Pwd,
		DB:       config.Redis.DB,
	}

	rs := monitor.NewRedisStore(ropts)
	if err := rs.Ping(); err != nil {
		return err
	}
	mon.StatusStore = rs

	b.Monitor = mon

	return nil
}

func main() {
	bot := telegrambot.Bot{}

	configPath := flag.String("config", "config.toml", "Path to the config file")
	flag.Parse()

	config, err := ReadConfig(*configPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	connection, err := gorm.Open("sqlite3", config.Database.Name)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bot.DB = &telegrambot.TargetsDB{
		DB: connection,
	}
	bot.DB.Migrate()

	bot.TgBot, err = tgbotapi.NewBotAPI(config.Telegram.APIKey)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bot.TgBot.Debug = config.Telegram.Debug
	bot.AdminNickname = config.Telegram.Admin

	err = monitorCreate(&bot, config)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bot.MonitorStart()

	err = bot.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
