package main

import (
        "github.com/Syfaro/telegram-bot-api"
        "gopkg.in/yaml.v2"
        "log"
        "io/ioutil"
)

type TypeConfig struct {
        Token string
        UserFile string
}

func ReadConfig(configFilename string) (*TypeConfig) {
	configBytes, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Panic(err)
	}
	configMap := make(map[interface{}]interface{})
	yaml.Unmarshal(configBytes, &configMap)
        var Config TypeConfig
        Config.Token = configMap["token"].(string)
        Config.UserFile = configMap["user_file"].(string)
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

func GetUpdatesChan(bot *tgbotapi.BotAPI) (<-chan tgbotapi.Update) {
        // initialize update chan
        c := tgbotapi.NewUpdate(0)
        c.Timeout = 60
        updates, err := bot.GetUpdatesChan(c)
        if err != nil {
                log.Panic(err)
        }
        return updates
}

func main() {
        // read config
        Config := *ReadConfig("config.yml")
        // connect to Telegram API
        bot := GetBot(Config.Token)
        // initialize update chan
        updates := GetUpdatesChan(bot)
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
                msg = tgbotapi.NewMessage(ChatID, "Мде...")
                bot.Send(msg)
        }
}

