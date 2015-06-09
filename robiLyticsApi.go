package main

import (
	"flag"
	"github.com/garyburd/redigo/redis"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"
	"encoding/json"
	"strconv"
)

var project_wg sync.WaitGroup
var errorLogFile string = "/var/log/robiLytics.error.log"
var debugLogFile string = "/var/log/robiLytics.debug.log"

func main() {

	numDevs := getNumDevelopers()
	report := flag.String("report", "", "Report to run")
	flag.Parse()

	// get the config data
	config := &JSONConfigData{}
	J, err := ioutil.ReadFile("config.json")
	if err != nil {
		errorToLog(errorLogFile, "Could not read config.json", err)
	}
	json.Unmarshal([]byte(J), &config)

	// Connect to Redis
	redisConn, err := redis.Dial("tcp", ":6379")
	if err != nil {
		errorToLog(errorLogFile, "Could not connect to Redis server", err)
	}

	switch *report {
	case "velocity":
		developer_wg.Add(numDevs)
		for _, team := range config.Teams {
			for _, developer := range team.Members {
				go getDeveloperVelocity(config, developer)
			}
		}
		developer_wg.Wait()

	case "meetings":
		developer_wg.Add(numDevs)
		for _, team := range config.Teams {
			for _, developer := range team.Members {
				go getWorklogData(config, developer)
			}
		}
		developer_wg.Wait()

	case "defectRatio":
		t := time.Now()
		date := t.Format("01/02/2006")
		year, week := t.ISOWeek()
		y := strconv.Itoa(year)
		w := strconv.Itoa(week)

		for _, team := range config.Teams {

			redisConn.Do("SADD", "data:teams", team.Name)
			var teamTotal float64 = 0
			var teamPop int = 0
			for _, developer := range team.Members {
				redisConn.Do("SADD", "data:developers", developer)
				redisConn.Do("SADD", "data:team:"+team.Name+":developers:", developer)
				ratio := getDeveloperDefectRatio(config, developer)
				redisConn.Do("HSET", "stats:defectRatio:developer:"+developer, w+":"+y, ratio)
				teamTotal = teamTotal + ratio
				teamPop++
			}
			teamAvg := teamTotal / float64(teamPop)
			redisConn.Do("HSET", "stats:defectRatio:team:"+team.Name, date, teamAvg)
		}
	case "redFlags":
		developer_wg.Add(numDevs * 3)
		for _, team := range config.Teams {
			for _, developer := range team.Members {
				go getActiveStoriesWithNoEstimate(config, developer)
				go getStoriesWithNoLoggedHrs(config, developer)
				go activeStoriesWithNoFixVersion(config, developer)
			}
		}
		developer_wg.Wait()

		project_wg.Add(len(config.Projects))
		for _, project := range config.Projects {
			go getActiveStoryEdits(config, project)
		}
		project_wg.Wait()
	}
}

func getWeekNumber(dateString string, delimiter string) (int, int) {
	s := strings.Split(dateString, delimiter)
	t, err := time.Parse("2006-01-02", s[0])
	if err != nil {
		errorToLog(errorLogFile, "Could not parse time string", err)
	}
	week, year := t.ISOWeek()
	return week, year
}

func cURLEndpoint(config *JSONConfigData, endpoint string) string {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		errorToLog(errorLogFile, "Could not connect to EP", err)
	}
	req.SetBasicAuth(config.Username, config.Password)
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		errorToLog(errorLogFile, "Could not authenticate to EP: "+endpoint, err)
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		errorToLog(errorLogFile, "Could not read EP response: "+string(body), err)
	}
	res.Body.Close()
	return string(body)
}


