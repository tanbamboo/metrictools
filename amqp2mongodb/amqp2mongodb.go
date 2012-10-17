package main

import (
	"flag"
	"fmt"
	"github.com/datastream/metrictools"
	"github.com/datastream/metrictools/amqp"
	"github.com/kless/goconfig/config"
	"log"
	"os"
)

var (
	conf_file = flag.String("conf", "metrictools.conf", "analyst config file")
)

const nWorker = 10

func main() {
	flag.Parse()
	c, err := config.ReadDefault(*conf_file)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	mongouri, _ := c.String("Generic", "mongodb")
	dbname, _ := c.String("Generic", "dbname")
	user, _ := c.String("Generic", "user")
	password, _ := c.String("Generic", "password")
	uri, _ := c.String("Generic", "amqpuri")
	exchange, _ := c.String("Generic", "exchange")
	exchange_type, _ := c.String("Generic", "exchange-type")
	queue, _ := c.String("amqp2mongo", "queue")
	binding_key, _ := c.String("amqp2mongo", "bindingkey")
	consumer_tag, _ := c.String("amqp2mongo", "consumertag")

	db_session := metrictools.NewMongo(mongouri, dbname, user, password)
	if db_session == nil {
		log.Println("connect database error")
		os.Exit(1)
	}
	for i := 0; i < nWorker; i++ {
		message_chan := make(chan *amqp.Message)
		consumer := amqp.NewConsumer(uri, exchange, exchange_type, queue, binding_key, consumer_tag)
		go consumer.Read_record(message_chan)
		session := db_session.Copy()
		go insert_record(message_chan, session, dbname)
	}
	select {}
}
