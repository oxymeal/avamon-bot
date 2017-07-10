package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/jinzhu/gorm"
	"github.com/yamnikov-oleg/avamon-bot/monitor"

	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

var (
	// Green clover
	okStatusEmoji = string([]rune{0x2618, 0xfe0f})
	// Red alarm light
	errorStatusEmoji = string([]rune{0x1f6a8})
)

func replaceHTML(input string) string {
	input = strings.Replace(input, "<", "&lt;", -1)
	input = strings.Replace(input, ">", "&gt;", -1)
	return input
}

type Bot struct {
	Config  *Config
	DB      *TargetsDB
	TgBot   *tgbotapi.BotAPI
	Monitor *monitor.Monitor
}

func (b *Bot) formatStatusUpdate(target monitor.Target, status monitor.Status) string {
	var output string
	var sign string

	if status.Type == monitor.StatusOK {
		sign = strings.Repeat(okStatusEmoji, 10) + "\n"
	} else {
		sign = strings.Repeat(errorStatusEmoji, 10) + "\n"
	}

	output += sign
	output += fmt.Sprintf("<b>%v:</b> <b>%v</b>\n\n", replaceHTML(target.Title), status.Type)
	output += fmt.Sprintf("<b>URL:</b> %v\n", replaceHTML(target.URL))
	output += fmt.Sprintf("<b>Время ответа:</b> %v\n", status.ResponseTime)

	if status.Type != monitor.StatusOK {
		output += fmt.Sprintf("<b>Сообщение:</b> %v\n", replaceHTML(status.Err.Error()))
	}
	if status.Type == monitor.StatusHTTPError {
		output += fmt.Sprintf("<b>Статус HTTP:</b> %v %v\n", status.HTTPStatusCode, http.StatusText(status.HTTPStatusCode))
	}
	output += sign

	return output
}

func (b *Bot) SendMessage(chatID int64, message string) {
	msg := tgbotapi.NewMessage(chatID, message)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	b.TgBot.Send(msg)
}

func (b *Bot) SendDialogMessage(replyTo *tgbotapi.Message, message string) {
	msg := tgbotapi.NewMessage(replyTo.Chat.ID, message)
	msg.ReplyToMessageID = replyTo.MessageID
	msg.ReplyMarkup = tgbotapi.ForceReply{
		ForceReply: true,
		Selective:  true,
	}
	b.TgBot.Send(msg)
}

func (b *Bot) monitorCreate() error {
	mon := monitor.New(b.DB)
	mon.Scheduler.Interval = time.Duration(b.Config.Monitor.Interval) * time.Second
	mon.Scheduler.ParallelPolls = b.Config.Monitor.MaxParallel
	mon.Scheduler.Poller.Timeout = time.Duration(b.Config.Monitor.Timeout) * time.Second
	mon.NotifyFirstOK = b.Config.Monitor.NotifyFirstOK

	ropts := monitor.RedisOptions{
		Host:     b.Config.Redis.Host,
		Port:     b.Config.Redis.Port,
		Password: b.Config.Redis.Pwd,
		DB:       b.Config.Redis.DB,
	}

	rs := monitor.NewRedisStore(ropts)
	if err := rs.Ping(); err != nil {
		return err
	}
	mon.StatusStore = rs

	b.Monitor = mon

	return nil
}

func (b *Bot) monitorStart() {
	go func() {
		for upd := range b.Monitor.Updates {
			var rec Record
			b.DB.DB.First(&rec, upd.Target.ID)
			b.SendMessage(
				rec.ChatID,
				b.formatStatusUpdate(upd.Target, upd.Status))
		}
	}()

	go func() {
		for err := range b.Monitor.Errors() {
			fmt.Println(err)
		}
	}()

	go b.Monitor.Run(nil)
}

type session struct {
	Stage  int
	Dialog dialog
}

type dialog interface {
	ContinueDialog(stepNumber int, update tgbotapi.Update, bot *tgbotapi.BotAPI) (int, bool)
}

type addNewTarget struct {
	Title string
	URL   string
	bot   *Bot
}

func (t *addNewTarget) ContinueDialog(stepNumber int, update tgbotapi.Update, bot *tgbotapi.BotAPI) (int, bool) {
	if stepNumber == 1 {
		t.bot.SendDialogMessage(update.Message, "Введите заголовок цели")
		return 2, true
	}
	if stepNumber == 2 {
		t.Title = update.Message.Text
		t.bot.SendDialogMessage(update.Message, "Введите URL адрес цели")
		return 3, true
	}
	if stepNumber == 3 {
		if _, err := url.Parse(update.Message.Text); err != nil {
			t.bot.SendDialogMessage(update.Message, "Ошибка ввода URL адреса, попробуйте еще раз")
			return 3, true
		}
		t.URL = update.Message.Text
		err := t.bot.DB.CreateTarget(Record{
			ChatID: update.Message.Chat.ID,
			Title:  t.Title,
			URL:    t.URL,
		})
		if err != nil {
			t.bot.SendMessage(
				update.Message.Chat.ID,
				fmt.Sprintf(
					"Ошибка добавления цели, свяжитесь с администратором: %v",
					t.bot.Config.Telegram.Admin))
			return 0, false
		}
		t.bot.SendMessage(update.Message.Chat.ID, "Цель успешно добавлена")
		return 0, false
	}
	return 0, false
}

type deleteTarget struct {
	bot *Bot
}

func (t *deleteTarget) ContinueDialog(stepNumber int, update tgbotapi.Update, bot *tgbotapi.BotAPI) (int, bool) {
	if stepNumber == 1 {
		targs, err := t.bot.DB.GetCurrentTargets(update.Message.Chat.ID)
		if err != nil {
			t.bot.SendMessage(
				update.Message.Chat.ID,
				fmt.Sprintf(
					"Ошибка получения целей, свяжитесь с администратором: %v",
					t.bot.Config.Telegram.Admin))
			return 0, false
		}
		if len(targs) == 0 {
			t.bot.SendMessage(update.Message.Chat.ID, "Целей не обнаружено!")
			return 0, false
		}
		var targetStrings []string
		targetStrings = append(targetStrings, "Введите <b>идентификатор</b> цели для удаления\n")
		for _, target := range targs {
			targetStrings = append(
				targetStrings,
				fmt.Sprintf(
					"<b>Идентификатор:</b> %v\n<b>Заголовок:</b> %v\n<b>URL:</b> %v\n",
					target.ID,
					replaceHTML(target.Title),
					replaceHTML(target.URL)))
		}
		message := strings.Join(targetStrings, "\n")
		t.bot.SendDialogMessage(update.Message, message)
		return 2, true
	}
	if stepNumber == 2 {
		target, err := strconv.Atoi(update.Message.Text)
		if err != nil {
			t.bot.SendDialogMessage(update.Message, "Ошибка ввода идентификатора")
			return 2, true
		}
		targetFromDB := Record{}
		err = t.bot.DB.DB.Where("ID = ?", target).First(&targetFromDB).Error
		if err != nil || targetFromDB.ChatID != update.Message.Chat.ID {
			t.bot.SendMessage(update.Message.Chat.ID, "Цель не найдена")
			return 0, false
		}
		err = t.bot.DB.DB.Where("ID = ?", target).Delete(Record{}).Error
		if err != nil {
			t.bot.SendMessage(
				update.Message.Chat.ID,
				fmt.Sprintf(
					"Ошибка удаления цели, свяжитесь с администратором: %v",
					t.bot.Config.Telegram.Admin))
			return 0, false
		}
		t.bot.SendMessage(update.Message.Chat.ID, "Цель успешно удалена!")
		return 0, false
	}
	return 0, false
}

func main() {
	bot := Bot{}

	configPath := flag.String("config", "config.toml", "Path to the config file")
	flag.Parse()

	var err error
	bot.Config, err = ReadConfig(*configPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	connection, err := gorm.Open("sqlite3", bot.Config.Database.Name)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bot.DB = &TargetsDB{
		DB: connection,
	}
	bot.DB.Migrate()

	bot.TgBot, err = tgbotapi.NewBotAPI(bot.Config.Telegram.APIKey)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bot.TgBot.Debug = bot.Config.Telegram.Debug
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 0

	updates, err := bot.TgBot.GetUpdatesChan(u)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = bot.monitorCreate()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	bot.monitorStart()

	var sessionMap = map[int64]*session{}
	for update := range updates {
		if update.Message == nil {
			continue
		}
		if _, ok := sessionMap[update.Message.Chat.ID]; !ok {
			sessionMap[update.Message.Chat.ID] = &session{}
			sessionMap[update.Message.Chat.ID].Stage = 1
			sessionMap[update.Message.Chat.ID].Dialog = nil
		}
		sess := sessionMap[update.Message.Chat.ID]
		if sess.Dialog != nil {
			var ok bool
			sess.Stage, ok = sess.Dialog.ContinueDialog(sess.Stage, update, bot.TgBot)
			if !ok {
				sess.Dialog = nil
			}
			continue
		}
		if update.Message.Command() == "start" {
			bot.SendMessage(
				update.Message.Chat.ID,
				"Привет!\nЯ бот который умеет следить за доступностью сайтов.\n")
			continue
		}
		if update.Message.Command() == "add" {
			var ok bool
			sess.Dialog = &addNewTarget{
				bot: &bot,
			}
			sess.Stage, ok = sess.Dialog.ContinueDialog(1, update, bot.TgBot)
			if !ok {
				sess.Dialog = nil
			}
			continue
		}
		if update.Message.Command() == "targets" {
			targs, err := bot.DB.GetCurrentTargets(update.Message.Chat.ID)
			if err != nil {
				bot.SendMessage(
					update.Message.Chat.ID,
					fmt.Sprintf(
						"Ошибка получения целей, свяжитесь с администратором: %v",
						bot.Config.Telegram.Admin))
				continue
			}
			if len(targs) == 0 {
				bot.SendMessage(update.Message.Chat.ID, "Целей не обнаружено!")
				continue
			}
			var targetStrings []string
			for _, target := range targs {
				status, ok, err := bot.Monitor.StatusStore.GetStatus(target.ToTarget())
				if err != nil {
					bot.SendMessage(
						update.Message.Chat.ID,
						fmt.Sprintf(
							"Ошибка статуса целей, свяжитесь с администратором: %v",
							bot.Config.Telegram.Admin))
					continue
				}

				var header string
				header = fmt.Sprintf(
					"<a href=\"%v\">%v</a>",
					replaceHTML(target.URL), replaceHTML(target.Title))

				var statusText string
				if ok {
					var emoji string
					if status.Type == monitor.StatusOK {
						emoji = okStatusEmoji
					} else {
						emoji = errorStatusEmoji
					}

					statusText = fmt.Sprintf(
						"%v %v (%v ms)",
						emoji, status.Type, int64(status.ResponseTime/time.Millisecond))
				} else {
					statusText = "N/A"
				}

				targetStrings = append(
					targetStrings, fmt.Sprintf("%v: %v", header, statusText))
			}
			message := strings.Join(targetStrings, "\n")
			bot.SendMessage(update.Message.Chat.ID, message)
			continue
		}
		if update.Message.Command() == "delete" {
			var ok bool
			sess.Dialog = &deleteTarget{
				bot: &bot,
			}
			sess.Stage, ok = sess.Dialog.ContinueDialog(1, update, bot.TgBot)
			if !ok {
				sess.Dialog = nil
			}
			continue
		}
	}
}
