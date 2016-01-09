package main

import (
        "github.com/Syfaro/telegram-bot-api"
        "gopkg.in/yaml.v2"
        "log"
        "io"
        "io/ioutil"
        "os"
        "os/user"
        "strings"
        "regexp"
        "net"
        "fmt"
)

var HelpMessage string = `Commands for manage nagios:
/help - This message
/start - Add telegram user to the bot roster
/hosts [d|u] - Without args list all problem host
/srvs [w|c|u] - Without args list all problem services
`

type TypeConfig struct {
        // Config
        Token string
        UserFile string
        LivestatusSocket string
        NagiosNotifySocket string
        NagiosUsernameField string
}

// tgbotapi.BotAPI clone for custom methods
type BotAPI struct {
        *tgbotapi.BotAPI
}

// Read from config file
func ReadConfig(configFilename string) (*TypeConfig) {
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

// connect to Telegram API
func GetBot(token string) (*BotAPI) {
        bot, err := tgbotapi.NewBotAPI(token)
        if err != nil {
                log.Panic(err)
        }
        bot.Debug = true
        log.Printf("Authorized on account %s", bot.Self.UserName)
        return &BotAPI{bot}
}

// initialize update chan
func GetUpdatesChan(bot *BotAPI) (<-chan tgbotapi.Update) {
        c := tgbotapi.NewUpdate(0)
        c.Timeout = 60
        updates, err := bot.GetUpdatesChan(c)
        if err != nil {
                log.Panic(err)
        }
        return updates
}

var Users map[string]int

// read registered users file
func ReadUsers(usersFileName string) {
        Users = make(map[string]int)
        usersBytes, err := ioutil.ReadFile(usersFileName)
	if err != nil {
		log.Print(err)
                return
	}
	yaml.Unmarshal(usersBytes, &Users)
}

// write registered users file
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

// Handle Chat message
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
                //commArgs := regexp.MustCompile(`\s+`).Split(matches[2], -1)
                switch comm {
                case `help`:
                        reply = HelpMessage
                case `start`:
                        reply = "Greetings to you, " + UserName + "!"
                case `hosts`:
                        reply, _ = GetNagiosAllProblemHosts(livestatusSocketFileName, NagiosUser)
                }
                if reply != `` {
                        msg := tgbotapi.NewMessage(ChatID, reply)
                        bot.Send(msg)
                }
        }
}

// Create socket and channel for nagios notify
func GetNotifyChan(nagiosSocketFileName string) (<-chan io.ReadCloser) {
        notifyesChan := make(chan io.ReadCloser, 100)
        // create listener
        go func() {
                // remove socket
                os.Remove(nagiosSocketFileName)
                // create socket
                listen, err := net.ListenUnix("unix",  &net.UnixAddr{nagiosSocketFileName, "unix"})
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

// Handle nagios notification
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
                msg := tgbotapi.NewMessage(ChatID, strings.Join(stringArray[1:],"\n"))
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

// Get all problem nagios hosts
func GetNagiosAllProblemHosts(livestatusSocketFileName string, nagiosUserName string) (string, bool) {
        if len(nagiosUserName) == 0 {
                return "", false
        }
        return GetNagiosHosts(livestatusSocketFileName, "Filter: hard_state = 1\nFilter: hard_state = 2\nOR: 2\n" ,nagiosUserName)
}

// Get standart host list with filters
func GetNagiosHosts(livestatusSocketFileName string, filters string, nagiosUserName string) (string, bool) {
        var buf [1024]byte
        var response, up, down, unreachable string
        // beware! filters MUST  ended by "\n"
        r, ok := GetLiveStatus(livestatusSocketFileName, fmt.Sprintf("GET hosts\nColumns: host_name hard_state \n%sAuthUser: %s\n\n", filters, nagiosUserName))
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
                                up += fields[0]+ "\n"
                        } else if fields[1] == "1" {
                                down += fields[0] + "\n"
                        } else {
                                unreachable += fields[0] + "\n"
                        }
                }
                hosts := ""
                if len(down) > 0 {
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
        } else {
                return "", false
        }
}

// Get Nagios <-> Telegram user identity
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
                // parse text for User name
                stringArray := strings.Split(response, "\n")
                if len(stringArray) == 0 {
                        return "", false
                }
                nagiosUser := stringArray[0]
                return nagiosUser, true
        } else {
                return "", false
        }
}

// Print command to LiveStatus UNIX-socket and return io.ReadCloser object
func GetLiveStatus(livestatusSocketFileName string, request string) (io.ReadWriteCloser, bool) {
        r, err := net.DialUnix("unix", nil, &net.UnixAddr{livestatusSocketFileName, "unix"})
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
                case update := <- Updates:
                        go Talks(Config.UserFile, Config.LivestatusSocket, Config.NagiosUsernameField, Bot, update)
                case notify := <- Notifyes:
                        go Notify(Config.UserFile, Bot, notify)
                }
        }
}

