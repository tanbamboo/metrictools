package main

import (
	"flag"
	"github.com/datastream/metrictools"
	"github.com/kless/goconfig/config"
	"labix.org/v2/mgo"
	"log"
	"net/http"
	"os"
)

var (
	conf_file = flag.String("conf", "metrictools.conf", "analyst config file")
)

const (
	METRIC = 1
	APP    = 2
)

var db_session *mgo.Session
var dbname string

func main() {
	flag.Parse()
	c, err := config.ReadDefault(*conf_file)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	mongouri, _ := c.String("Generic", "mongodb")
	dbname, _ = c.String("Generic", "dbname")
	user, _ := c.String("Generic", "user")
	password, _ := c.String("Generic", "password")
	port, _ := c.String("web", "port")

	db_session = metrictools.NewMongo(mongouri, dbname, user, password)
	if db_session == nil {
		log.Println("connect database error")
		os.Exit(1)
	}

	http.HandleFunc("/monitorapi/metric", metric_controller)
	http.HandleFunc("/monitorapi/types", type_controller)
	http.HandleFunc("/monitorapi/relation", relation_controller)
	http.HandleFunc("/monitorapi/alarm", alarm_controller)

	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Println(err)
	}
}