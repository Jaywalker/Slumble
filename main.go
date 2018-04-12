//This program functions as both a slack and mumble bot for the purposes of conversation relay (and thus backup in Slack)
package main

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/grokify/html-strip-tags-go"
	"github.com/nlopes/slack"
	"github.com/tkanos/gonfig"
	"layeh.com/gumble/gumble"
	"layeh.com/gumble/gumbleutil"
)

type Configuration struct {
	SlackAPIToken string
	SlackChannel  string
}

var config Configuration
var mumbleMessageChan chan string
var slackApi *slack.Client
var slackRtm *slack.RTM
var mumbleClient *gumble.Client

//connectSlack is the main function of the Slack API connector.
//It first connects to slack using the API token, then starts the RTM connection manager in a go-thread.
//It then creates go-thread instance of manageSlack for handling the incoming SlackAPI messages.
//Finally, it enters its main loop which waits for messages coming in on MumbleMessageChan from the gumble connection.
//This function should only return if a message comes down on a channel that doesn't exist yet, so, never.
func connectSlack() {
	var token = config.SlackAPIToken
	slackApi = slack.New(token)
	slackRtm = slackApi.NewRTM()
	go slackRtm.ManageConnection()
	go manageSlack()
	for {
		msg := <-mumbleMessageChan
		if msg != "" {
			processedMsg := msg
			//Image decoding block. Mumble uses inline base64
			if strings.Contains(msg, "<img src=") {
				images := strings.SplitN(msg, `<img src="`, -1)
				for _, img := range images {
					if strings.HasPrefix(img, "data:image/") {
						tmp := strings.Split(img, "base64,")
						base64str := tmp[1]
						tmp = strings.Split(base64str, `"/>`)
						base64str = strings.Replace(tmp[0], " ", "", -1)
						base64str, _ = url.PathUnescape(base64str)
						//fmt.Println("UnBase64:", base64str)
						decoded, err := base64.StdEncoding.DecodeString(base64str)
						if err != nil {
							fmt.Println("decode error:", err)
							//return
						} else {
							params := slack.FileUploadParameters{
								Title:   "Mumble Image ",
								Content: string(decoded[:]),
							}
							_, err := slackApi.UploadFile(params)
							if err != nil {
								fmt.Printf("%s\n", err)
							}
						}
					}
				}
				fmt.Println(processedMsg)
			}
			fmt.Println(processedMsg)
			processedMsg = strip.StripTags(processedMsg)
			slackApi.PostMessage(config.SlackChannel, processedMsg, slack.PostMessageParameters{AsUser: true})
		}
	}
}

//This function reads all incoming events from the slackRtm variable
//The events we currently handle (not just log) are:
//	slack.MessageEvent
//	slack.FileCommentAddedEvent
//This function should only return if provided invalid credentials
func manageSlack() {
	for msg := range slackRtm.IncomingEvents {
		fmt.Print("Slack Event Received: ")
		switch ev := msg.Data.(type) {
		case *slack.HelloEvent:
			// Ignore hello
		case *slack.ConnectedEvent:
			fmt.Println("Infos:", ev.Info)
			fmt.Println("Connection counter:", ev.ConnectionCount)
		case *slack.MessageEvent:
			slackUser, err := slackApi.GetUserInfo(ev.User)
			if err != nil {
				fmt.Printf("Error retrieving slack user: %s\n", err)
			} else {
				if slackUser.Name != "mumblerelay" {
					//Mumble accepts HTML, Slack (just API; IRC uses plain text) uses their own weird formatting. Lets fix that.
					var re = regexp.MustCompile(`<(http[\$\+\!\*\'\(\)\,\?\=%\_a-zA-Z0-9\/\.:-]*)\|?(.*)?>`)
					text := re.ReplaceAllString(ev.Text, `<a href="$1">$2</a>`)
					text = strings.Replace(text, `"></a>`, `">Link has no title? I didn't know Slack would even do that...</a>`, -1)
					msg := slackUser.Name + ": " + text
					mumbleClient.Self.Channel.Send(msg, false)
					fmt.Println(msg)
				}
			}
		case *slack.PresenceChangeEvent:
			fmt.Printf("Presence Change: %v\n", ev)
		case *slack.LatencyReport:
			fmt.Printf("Current latency: %v\n", ev.Value)
		case *slack.RTMError:
			fmt.Printf("Error: %s\n", ev.Error())
		case *slack.DisconnectedEvent:
			//Nothing yet...
		case *slack.FileCommentEditedEvent:
			//Maybe
			fmt.Printf("FileCommentEdited: %v\n", msg.Data)
		case *slack.FilePublicEvent:
			//Maybe
			fmt.Printf("FilePublic: %v\n", msg.Data)
		case *slack.FileSharedEvent:
			//Maybe
			fmt.Printf("FileShared: %v\n", msg.Data)
		case *slack.ChannelJoinedEvent:
			//Maybe
			//Perhaps when I join a non-configured channel, I complain and leave..
			//Or better yet, enter spy mode; Read history, members and future messages :P
			fmt.Printf("ChannelJoined: %v\n", msg.Data)
		case *slack.ReactionAddedEvent:
			//Maybe
			fmt.Printf("ReactionAdded: %v\n", msg.Data)
		case *slack.MessageTooLongEvent:
			//Maybe
			fmt.Printf("MessageTooLong: %v\n", msg.Data)
		case *slack.FileCommentAddedEvent:
			fmt.Printf("File Comment Added: %s: %s\n", ev.Comment.User, ev.Comment.Comment)
		case *slack.InvalidAuthEvent:
			fmt.Printf("Invalid credentials")
			return
		default:
			// Ignore other events..
			fmt.Printf("Unexpected: %v\n", msg.Data)
		}
	}
}

func main() {
	config = Configuration{}
	err := gonfig.GetConf("slumble.config", &config)
	if err != nil {
		//Probably should add some sort of config creation here
		panic(err)
	}
	mumbleMessageChan = make(chan string)
	go connectSlack()
	gumbleutil.Main(gumbleutil.AutoBitrate, gumbleutil.Listener{
		Connect: func(e *gumble.ConnectEvent) {
			//Give our mumble client to the global scope
			mumbleClient = e.Client
		},
		TextMessage: func(e *gumble.TextMessageEvent) {
			if e.Sender == nil {
				return
			}
			if e.Sender.Name != "SlackRelay" {
				//We got a message and its not from us, Lets pass it to our mumbleMessageChan to get picked up by connectSlack
				msg := e.Sender.Name + ": " + e.Message
				mumbleMessageChan <- msg
				//Debug
				//fmt.Println(msg)
			}
		},
	})
}
