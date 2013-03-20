package main

import (
	metrictools "../"
	"labix.org/v2/mgo/bson"
	"log"
	"net/http"
)

func StatisticHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=\"utf-8\"")
	name := req.FormValue("name") // all
	starttime := req.FormValue("start")
	endtime := req.FormValue("end")
	start := gettime(starttime)
	end := gettime(endtime)
	if !checktime(start, end) {
		start = end - 3600*3
	}
	session := db_session.Clone()
	defer session.Close()
	var query []metrictools.Record
	err := session.DB(dbname).C(statistic_collection).
		Find(bson.M{"k": name,
		"t": bson.M{"$gt": start, "$lt": end}}).Sort("t").All(&query)
	if err != nil {
		log.Printf("query metric error:%s\n", err)
		db_session.Refresh()
	} else {
		w.Write(json_metrics_value(query))
	}
}
