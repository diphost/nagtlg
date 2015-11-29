package main

import (
        "github.com/Syfaro/telegram-bot-api"
        "gopkg.in/yaml.v2"
        "log"
        "io/ioutil"
        "strconv"
)

func ReadConfig(cf string) (*map[interface{}]interface{}) {
	cfg, err := ioutil.ReadFile(cf)
	if err != nil {
		log.Panic(err)
	}
	Config := make(map[interface{}]interface{})
	yaml.Unmarshal(cfg, &Config)
	return &Config
}

func GetBot(token string) (*tgbotapi.BotAPI) {
        // connect to Telegram API
        bot, err := tgbotapi.NewBotAPI(token)
        if err != nil {
                log.Panic(err)
        }
        bot.Debug = true
        log.Printf("Authorized on account %s", bot.Self.UserName)
        return bot
}

func GetUpdatesChan(bot *tgbotapi.BotAPI, state_file string) (<-chan tgbotapi.Update) {
        state, err := ioutil.ReadFile(state_file)
        if err != nil {
                log.Print(err)
        }
        last_id, err := strconv.Atoi(string(state))
        if err != nil {
                last_id = 0
        }
        // initialize update chan
        uc := tgbotapi.NewUpdate(last_id)
        uc.Timeout = 60
        updates, err := bot.GetUpdatesChan(uc)
        if err != nil {
                log.Panic(err)
        }
        return updates
}

func main() {
        // read config
        Config := *ReadConfig("config.yml")
        // connect to Telegram API
        bot := GetBot(Config["token"].(string))
        // initialize update chan
        updates := GetUpdatesChan(bot, Config["state_file"].(string))
        // read updates
        for {
                update := <- updates
                // who writing
                UserName := update.Message.From.UserName

                // ID of chat/dialog
                // maiby eq UserID or public chat
                ChatID := update.Message.Chat.ID

                // message text
                Text := update.Message.Text

                log.Printf("[%s] %d %s", UserName, ChatID, Text)

                // echo
                reply := Text
                // create answer message
                msg := tgbotapi.NewMessage(ChatID, reply)
                // send message
                bot.Send(msg)
                msg = tgbotapi.NewMessage(ChatID, "Мамель, мы тебя любим!")
                bot.Send(msg)
        }
}

