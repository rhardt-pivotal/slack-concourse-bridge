package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/nlopes/slack"
	"github.com/nlopes/slack/slackevents"
	"github.com/concourse/go-concourse/concourse"

	"time"
	"crypto/tls"
	"encoding/base64"
	"net/http/httputil"
	"io/ioutil"
	"log"
	"regexp"
)

// You more than likely want your "Bot User OAuth Access Token" which starts with "xoxb-"
var api = slack.New(os.Getenv("SLACK_BOT_TOKEN"))

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.HandleFunc("/events-endpoint", func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		body := buf.String()
		fmt.Printf("%s\n", body)
		eventsAPIEvent, e := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionVerifyToken(&slackevents.TokenComparator{os.Getenv("VERIFICATION_TOKEN")}))
		if e != nil {
			fmt.Printf("%+v\n", e)
			w.WriteHeader(http.StatusInternalServerError)
		}

		if eventsAPIEvent.Type == slackevents.URLVerification {
			var r *slackevents.ChallengeResponse
			err := json.Unmarshal([]byte(body), &r)
			if err != nil {
				fmt.Printf("%+v\n", e)
				w.WriteHeader(http.StatusInternalServerError)
			}
			w.Header().Set("Content-Type", "text")
			w.Write([]byte(r.Challenge))
		}
		if eventsAPIEvent.Type == slackevents.CallbackEvent {
			postParams := slack.PostMessageParameters{}
			innerEvent := eventsAPIEvent.InnerEvent
			switch ev := innerEvent.Data.(type) {
			case *slackevents.AppMentionEvent:

				if ev.User != "" {
					user, e := api.GetUserInfo(ev.User)
					if e != nil {
						api.PostMessage(ev.Channel, fmt.Sprintf("Unable to lookup user: %s, doing nothing.", ev.User), postParams)
						return
					} else {
						if strings.Contains(strings.ToUpper(ev.Text), "STOP") {
							log.Println(ev.Text)
							log.Printf("** User: %+v", ev.User)

							api.PostMessage(ev.Channel, fmt.Sprintf("%s, you appear to want to stop the build.  Lemme try...", user.Name), postParams)
							reg := regexp.MustCompile("(?i).+stop ([0-9]+).*")
							result := reg.FindAllStringSubmatch(ev.Text, -1)
							if len(result) < 1 || len(result[0]) < 2{
								api.PostMessage(ev.Channel, "Expected '<@UBKJFT7E0> stop <number>', but didn't find it", postParams)
								return
							}
							buildId := result[0][1]
							log.Printf("Build ID: %s", buildId)
							tr := http.DefaultTransport
							httpClient := &http.Client{Transport: tr, Timeout: time.Second * 180,}
							req, e := http.NewRequest("GET", os.Getenv("CI_URI")+"/auth/basic/token?team_name="+os.Getenv("CI_TEAM_NAME"), nil)
							req.SetBasicAuth(os.Getenv("CI_USER"), os.Getenv("CI_PASSWORD"))
							if e != nil {
								msg := fmt.Sprintf("error creating request: %+v", e)
								api.PostMessage(ev.Channel, msg, postParams)
								return
							}

							b, err := httputil.DumpRequestOut(req, true)
							if err != nil {
								fmt.Printf("request error: %+v", err)
								return
							}
							fmt.Println(string(b))

							resp, err := httpClient.Do(req)

							if err != nil {
								fmt.Printf("error response from server: %+v", err)
								return
							}

							fmt.Println("GOT RESPONSE")

							body, readErr := ioutil.ReadAll(resp.Body)
							if readErr != nil {
								log.Fatal(readErr)
								return
							}

							var objmap map[string]string
							uerr := json.Unmarshal(body, &objmap)

							log.Printf("objmap: %+v", objmap)

							if uerr != nil {
								log.Fatal(uerr)
								return
							}

							client := concourse.NewClient(os.Getenv("CI_URI"), httpClient, true, string(objmap["value"]))

							build, running, err := client.Build(buildId)

							if err != nil {
								api.PostMessage(ev.Channel, fmt.Sprintf("Error looking up build: %v+", err), postParams)
								return
							} else if !running {
								api.PostMessage(ev.Channel, "Build doesn't appear to be in progress", postParams)
								return
							} else {
								api.PostMessage(ev.Channel, fmt.Sprintf("attempting to stop build %v+", build), postParams)
								err := client.AbortBuild(buildId)
								if err == nil {
									api.PostMessage(ev.Channel, "success", postParams)
								} else {
									api.PostMessage(ev.Channel, fmt.Sprintf("error trying to stop the build: %v+", err), postParams)
								}
							}
						}
					}

					if e == nil {

					} else {
						api.PostMessage(ev.Channel, fmt.Sprintf("Hello, %s. How are you?  Did you need something?  Right now I only know how to stop the build in progress.  Please notify me with a message containing the work `stop`.  Thanks!", user.Name), postParams)
					}
				}

			}
		}
	})
	fmt.Println("[INFO] Server listening")
	http.ListenAndServe(":8080", nil)
}



func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
