package es

import (
	"context"
	"fmt"
	"github.com/olivere/elastic/v7"
	"kp-collector/internal/pkg/dal/kao"
	log2 "kp-collector/internal/pkg/log"
	"log"
	"os"
	"strconv"
	"time"
)

var Client *elastic.Client

func InitEsClient(host, user, password string) {
	Client, _ = elastic.NewClient(
		elastic.SetURL(host),
		elastic.SetSniff(false),
		elastic.SetBasicAuth(user, password),
		elastic.SetErrorLog(log.New(os.Stdout, "APP", log.Lshortfile)),
		elastic.SetHealthcheckInterval(30*time.Second),
	)
	_, _, err := Client.Ping(host).Do(context.Background())
	if err != nil {
		panic(fmt.Sprintf("es连接失败: %s", err))
	}
	return
}

func InsertTestData(sceneTestResultDataMsg *kao.SceneTestResultDataMsg) {

	index := strconv.FormatInt(sceneTestResultDataMsg.TeamId, 10)
	exist, err := Client.IndexExists(index).Do(context.Background())
	if err != nil {
		panic(fmt.Sprintf("es连接失败: %s", err))
	}
	if !exist {
		_, err := Client.CreateIndex(index).Do(context.Background())
		if err != nil {
			log2.Logger.Error("es创建索引", index, "失败", err)
			return
		}
	}
	_, err = Client.Index().Index(index).BodyJson(sceneTestResultDataMsg).Do(context.Background())
	if err != nil {
		log2.Logger.Error("es写入数据失败", err)
		return
	}

}
