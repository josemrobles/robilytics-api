package main

import (
	"encoding/json"
	"github.com/garyburd/redigo/redis"
	"strconv"
	"sync"
)

var developer_wg sync.WaitGroup

func checkDevs(redisConn redis.Conn, config *JSONConfigData) {
	for _, team := range config.Teams {
		for _, developer := range team.Members {
			redisConn.Do("SADD", "data:developers", developer)
		}
	}
}

func getDeveloperDefectRatio(config *JSONConfigData, developer string) float64 {

	endpoint := config.Url
	endpoint += "search?jql=assignee="
	endpoint += developer
	endpoint += "&maxResults=2000"
	endpoint += "&expand=changelog"
	endpoint += "&orderby=created"
	jiraApiResponse := cURLEndpoint(config, endpoint)

	jiraStoryData := &jiraDataStruct{}
	json.Unmarshal([]byte(jiraApiResponse), &jiraStoryData)

	var delivered int = 0
	var rejected int = 0

	for _, issue := range jiraStoryData.Issues {
		for _, history := range issue.ChangeLog.Histories {
			for _, item := range history.Items {
				if item.Field == "status" && item.FromString == "Accepted" && item.ToString == "Rejected" {
					rejected++
				}

				if item.Field == "status" && item.ToString == "Accepted" {
					delivered++
				}
			}
		}
	}
	result := float64(rejected) / float64(delivered)
	return result
}

func getDeveloperVelocity(config *JSONConfigData, developer string, redisConn redis.Conn) {

	var y string = ""
	var w string = ""

	endpoint := config.Url
	endpoint += "search?jql=assignee="
	endpoint += developer
	endpoint += "&maxResults=20000"
	endpoint += "&expand=changelog"
	endpoint += "&orderby=created"
	jiraApiResponse := cURLEndpoint(config, endpoint)

	jiraStoryData := &jiraDataStruct{}
	json.Unmarshal([]byte(jiraApiResponse), &jiraStoryData)

	for _, issue := range jiraStoryData.Issues {
		check, err := redis.Int(redisConn.Do("SISMEMBER", "data:velocityLogs:developer:"+developer, issue.Id))
		if err != nil {
			errorToLog(errorLogFile, "Could not match issue id against velocityLogs", err)
		}
		if check == 0 {
			for _, history := range issue.ChangeLog.Histories {
				year, week := getWeekNumber(history.Created, "T")
				y = strconv.Itoa(year)
				w = strconv.Itoa(week)
				for _, item := range history.Items {
					if item.Field == "status" && item.ToString == "Finished" && issue.Fields.CustomField_10004 > 0 {
						redisConn.Do("HINCRBY", "data:velocity:developer:"+developer, w+":"+y+":TOTAL", issue.Fields.CustomField_10004)
					}
				}
			}
			total, err := redis.Int(redisConn.Do("HGET", "data:velocity:developer:"+developer, w+":"+y+":TOTAL"))
			if err != nil {
				errorToLog(errorLogFile, "Could not get the total number of hours:"+developer+" "+w+" "+y, err)
			}
			if total > 0 {
				redisConn.Do("HSET", "stats:velocity:developer:"+developer, w+":"+y, total)
				redisConn.Do("SADD", "data:velocityLogs:developer:"+developer, issue.Id)
			}
		}
	}
	defer developer_wg.Done()
}

func getWorklogData(config *JSONConfigData, developer string, redisConn redis.Conn) {

	ep1 := config.Url
	ep1 += "search?jql=assignee="
	ep1 += developer
	ep1 += "%20and%20issueType=Meeting"
	ep1 += "%20and%20status=Doing"
	jiraApiResponse := cURLEndpoint(config, ep1)

	jiraStoryData := &jiraDataStruct{}
	json.Unmarshal([]byte(jiraApiResponse), &jiraStoryData)

	for _, issue := range jiraStoryData.Issues {
		ep2 := config.Url
		ep2 += "issue/"
		ep2 += issue.Key
		ep2 += "/worklog"
		jiraApiResponse := cURLEndpoint(config, ep2)

		jiraWorklogData := &jiraWorklogStruct{}
		json.Unmarshal([]byte(jiraApiResponse), &jiraWorklogData)

		for _, worklog := range jiraWorklogData.Worklogs {
			check, err := redis.Int(redisConn.Do("SISMEMBER", "data:workLogs:developer:"+developer, worklog.Id))
			if err != nil {
				errorToLog(errorLogFile, "Could not match worklogID againt worklog SET for "+developer, err)
			}
			if check == 0 {
				year, week := getWeekNumber(worklog.Created, "T")
				y := strconv.Itoa(year)
				w := strconv.Itoa(week)
				redisConn.Do("HINCRBY", "stats:meetings:developer:"+developer, w+":"+y, worklog.TimeSpentSeconds/60)
				redisConn.Do("SADD", "data:workLogs:developer:"+developer, worklog.Id)
			}
		}
	}
	defer developer_wg.Done()
}

func getNumDevelopers(redisConn redis.Conn) int {
	numDevelopers, err := redis.Int(redisConn.Do("SCARD", "data:developers"))
	if err != nil {
		errorToLog(errorLogFile, "Cannot obtain the number of developers from data:developers SET", err)
	}
	return numDevelopers
}
