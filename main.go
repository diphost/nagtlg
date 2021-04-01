package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"regexp"
	"strings"

	tgbotapi "github.com/Syfaro/telegram-bot-api"
	"gopkg.in/yaml.v2"
)

// HelpMessage -
var HelpMessage string = `Commands for manage nagios:
/help - This message
/start - Add telegram user to the bot roster
/hosts [d|u] - Without args list all problem host
/srvs [w|c|u] - Without args list all problem services
`

// TypeConfig -
type TypeConfig struct {
	// Config
	Token               string
	UserFile            string
	LivestatusSocket    string
	NagiosNotifySocket  string
	NagiosUsernameField string
}

// BotAPI - tgbotapi.BotAPI clone for custom methods
type BotAPI struct {
	*tgbotapi.BotAPI
}

// ReadConfig - Read from config file
func ReadConfig(configFilename string) *TypeConfig {
	// config.yml default
	configBytes, err := ioutil.ReadFile(configFilename)
	if err != nil {
		log.Panic(err)
	}
	// get current user $HOME
	userCurrent, _ := user.Current()
	homeDir := userCurrent.HomeDir
	// YAML to data
	configMap := make(map[interface{}]interface{})
	yaml.Unmarshal(configBytes, &configMap)
	// init Config
	var config TypeConfig
	// get Telegram API token
	config.Token = configMap["Token"].(string)
	// get nagios contact field name
	config.NagiosUsernameField = configMap["NagiosUsernameField"].(string)
	// get registered users filename
	config.UserFile = configMap["UserFile"].(string)
	// expand "~"
	if config.UserFile[:2] == "~/" {
		config.UserFile = strings.Replace(config.UserFile, "~", homeDir, 1)
	}
	// get livestatus socket filename
	config.LivestatusSocket = configMap["LivestatusSocket"].(string)
	// expand "~"
	if config.LivestatusSocket[:2] == "~/" {
		config.LivestatusSocket = strings.Replace(config.LivestatusSocket, "~", homeDir, 1)
	}
	// get socket filename for nagios notify
	config.NagiosNotifySocket = configMap["NagiosNotifySocket"].(string)
	// expand "~"
	if config.NagiosNotifySocket[:2] == "~/" {
		config.NagiosNotifySocket = strings.Replace(config.NagiosNotifySocket, "~", homeDir, 1)
	}
	return &config
}

// GetBot - connect to Telegram API
func GetBot(token string) *BotAPI {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)
	return &BotAPI{bot}
}

// GetUpdatesChan - initialize update chan
func GetUpdatesChan(bot *BotAPI) <-chan tgbotapi.Update {
	c := tgbotapi.NewUpdate(0)
	c.Timeout = 60
	updates, err := bot.GetUpdatesChan(c)
	if err != nil {
		log.Panic(err)
	}
	return updates
}

// Users -
var Users map[string]int64

// ReadUsers - read registered users file
func ReadUsers(usersFileName string) {
	Users = make(map[string]int64)
	usersBytes, err := ioutil.ReadFile(usersFileName)
	if err != nil {
		log.Print(err)
		return
	}
	yaml.Unmarshal(usersBytes, &Users)
}

// WriteUsers - write registered users file
func WriteUsers(usersFileName string) {
	usersBytes, err := yaml.Marshal(&Users)
	if err != nil {
		log.Panic(err)
	}
	err = ioutil.WriteFile(usersFileName, usersBytes, 0600)
	if err != nil {
		log.Print(err)
	}
}

// Talks - Handle Chat message
func Talks(usersFileName string, livestatusSocketFileName string, nagiosUsernameField string, bot *BotAPI, update tgbotapi.Update) {
	// who writing
	UserName := update.Message.From.UserName
	// ID of chat/dialog
	// maiby eq UserID or public chat
	ChatID := update.Message.Chat.ID
	// get nagios user
	NagiosUser, _ := GetNagiosUser(livestatusSocketFileName, nagiosUsernameField, UserName)
	_, ok := Users[UserName]
	// write User to file, if absent
	if ok == false {
		Users[UserName] = ChatID
		WriteUsers(usersFileName)
	}
	log.Println(UserName + " = " + NagiosUser)
	// message text
	Text := update.Message.Text
	//log.Printf("[%s] %d %s", UserName, ChatID, Text)
	regex, _ := regexp.Compile(`^/([A-Za-z\_]+)\s*(.*)$`)
	matches := regex.FindStringSubmatch(Text)
	// hanlde chat commands
	if len(matches) > 0 {
		var reply string
		comm := matches[1]
		commArgs := regexp.MustCompile(`\s+`).Split(matches[2], -1)
		switch comm {
		case `help`:
			reply = HelpMessage
		case `start`:
			reply = "Greetings to you, " + UserName + "!"
		case `hosts`:
			filter := "Filter: hard_state = 1\nFilter: hard_state = 2\nOr: 2\n"
			if len(commArgs) > 0 {
				switch commArgs[0] {
				case `d`:
					filter = "Filter: hard_state = 1\n"
				case `u`:
					filter = "Filter: hard_state = 2\n"
				}
			}
			if len(NagiosUser) == 0 {
				reply = ``
			} else {
				reply, _ = GetNagiosHosts(livestatusSocketFileName, filter, NagiosUser)
			}
		}
		if reply != `` {
			msg := tgbotapi.NewMessage(ChatID, reply)
			bot.Send(msg)
		}
	}
}

// GetNotifyChan - Create socket and channel for nagios notify
func GetNotifyChan(nagiosSocketFileName string) <-chan io.ReadCloser {
	notifyesChan := make(chan io.ReadCloser, 100)
	// create listener
	go func() {
		// remove socket
		os.Remove(nagiosSocketFileName)
		// create socket
		uaddr, err := net.ResolveUnixAddr("unix", nagiosSocketFileName)
		if err != nil {
			log.Panic(err)
		}
		listen, err := net.ListenUnix("unix", uaddr)
		if err := os.Chmod(nagiosSocketFileName, 0777); err != nil {
			log.Panic(err)
		}
		if err != nil {
			log.Panic(err)
		}
		// remove socket on normal halt
		defer os.Remove(nagiosSocketFileName)
		// accept connection and send it in channel
		for {
			conn, err := listen.AcceptUnix()
			if err != nil {
				log.Panic(err)
			}
			notifyesChan <- conn
		}
	}()
	return notifyesChan
}

// Notify - Handle nagios notification
func Notify(usersFileName string, bot *BotAPI, notify io.ReadCloser) {
	defer notify.Close()
	var Text string
	var buf [1024]byte
	// read notification
	for {
		n, err := notify.Read(buf[:])
		// TODO error handling
		if err == io.EOF {
			break
		}
		Text += string(buf[:n])
	}
	//notify.Close()
	// parse text for User name
	stringArray := strings.Split(Text, "\n")
	if len(stringArray) < 2 {
		return
	}
	UserName := stringArray[0]
	if len(UserName) == 0 {
		return
	}
	// send notify to user, if user in our "roster"
	if ChatID, ok := Users[UserName]; ok {
		msg := tgbotapi.NewMessage(ChatID, strings.Join(stringArray[1:], "\n"))
		if len(msg.Text) == 0 {
			return
		}
		_, err := bot.Send(msg)
		if err != nil {
			matched, _ := regexp.MatchString(`Bad Request\: chat not found$`, err.Error())
			if matched {
				delete(Users, UserName)
				WriteUsers(usersFileName)
			}
		}
	}
}

// GetNagiosHosts - Get standart host list with filters
func GetNagiosHosts(livestatusSocketFileName string, filters string, nagiosUserName string) (string, bool) {
	var buf [1024]byte
	var response, up, down, unreachable string
	// beware! filters MUST  ended by "\n"
	r, ok := GetLiveStatus(livestatusSocketFileName, fmt.Sprintf("GET hosts\nColumns: host_name hard_state\n%sAuthUser: %s\n\n", filters, nagiosUserName))
	if ok {
		defer r.Close()
		for {
			n, err := r.Read(buf[:])
			// TODO error handling
			if err == io.EOF {
				break
			}
			response += string(buf[:n])
		}
		log.Println("Livestatus RESP:\n" + response + "\n")
		stringArray := strings.Split(response, "\n")
		if len(stringArray) == 0 {
			return "", false
		}
		for _, line := range stringArray {
			if len(line) == 0 {
				continue
			}
			fields := strings.Split(line, ";")
			if len(fields) != 2 {
				continue
			}
			if fields[1] == "0" {
				up += fields[0] + "\n"
			} else if fields[1] == "1" {
				down += fields[0] + "\n"
			} else {
				unreachable += fields[0] + "\n"
			}
		}
		hosts := ""
		if len(unreachable) > 0 {
			hosts += "UNREACHABLE HOSTS:\n"
			hosts += unreachable + "\n"
		}
		if len(down) > 0 {
			hosts += "DOWN HOSTS:\n"
			hosts += down + "\n"
		}
		if len(up) > 0 {
			hosts += "UP HOSTS:\n"
			hosts += up + "\n"
		}
		if len(hosts) == 0 {
			hosts = "No hosts matches\n"
		}
		return hosts, true
	}
	return "", false
}

// GetNagiosUser - Get Nagios <-> Telegram user identity
func GetNagiosUser(livestatusSocketFileName string, nagiosUsernameFieldName string, userName string) (string, bool) {
	var buf [1024]byte
	var response string
	r, ok := GetLiveStatus(livestatusSocketFileName, fmt.Sprintf("GET contacts\nColumns: name\nFilter: %s = %s\n\n", nagiosUsernameFieldName, userName))
	if ok {
		defer r.Close()
		for {
			n, err := r.Read(buf[:])
			// TODO error handling
			if err == io.EOF {
				break
			}
			response += string(buf[:n])
		}
		log.Println("Livestatus RESP:\n" + response + "\n")
		// parse text for User name
		stringArray := strings.Split(response, "\n")
		if len(stringArray) == 0 {
			return "", false
		}
		nagiosUser := stringArray[0]
		return nagiosUser, true
	}
	return "", false

}

// GetLiveStatus - Print command to LiveStatus UNIX-socket and return io.ReadCloser object
func GetLiveStatus(livestatusSocketFileName string, request string) (io.ReadWriteCloser, bool) {
	log.Println("Livestatus PUT:\n" + request + "\n")
	uaddr, err := net.ResolveUnixAddr("unix", livestatusSocketFileName)
	if err != nil {
		log.Panic(err)
	}
	r, err := net.DialUnix("unix", nil, uaddr)
	if err != nil {
		log.Println(err)
		return nil, false
	}
	_, err = r.Write([]byte(request))
	if err != nil {
		log.Println(err)
		return nil, false
	}
	return r, true
}

func main() {
	// read config
	// TODO get filename from command line
	Config := *ReadConfig("config.yml")
	// connect to Telegram API
	Bot := GetBot(Config.Token)
	// init Users cache
	ReadUsers(Config.UserFile)
	// init update chan
	Updates := GetUpdatesChan(Bot)
	// init notify chan
	Notifyes := GetNotifyChan(Config.NagiosNotifySocket)
	// read updates
	for {
		select {
		case update := <-Updates:
			go Talks(Config.UserFile, Config.LivestatusSocket, Config.NagiosUsernameField, Bot, update)
		case notify := <-Notifyes:
			go Notify(Config.UserFile, Bot, notify)
		}
	}
}
