package server

import (
	"RunnerGo-collector/internal/pkg/conf"
	"RunnerGo-collector/internal/pkg/dal/kao"
	"RunnerGo-collector/internal/pkg/dal/redis"
	log2 "RunnerGo-collector/internal/pkg/log"
	"encoding/json"
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/shopspring/decimal"
	"sort"
	"sync"
	"time"
)

func Execute(host string) {
	var partitionMap = new(sync.Map)
	start := conf.Conf.Kafka.Start
	end := conf.Conf.Kafka.End
	topic := conf.Conf.Kafka.Topic
	var list []int32
	for i := start; i <= end; i++ {
		list = append(list, i)
	}

	for {
		for _, value := range list {
			if _, ok := partitionMap.Load(value); ok {
				continue
			}
			saramaConfig := sarama.NewConfig()
			saramaConfig.Consumer.Return.Errors = true

			consumer, consumerErr := sarama.NewConsumer([]string{host}, sarama.NewConfig())
			if consumerErr != nil {
				log2.Logger.Error("topic  :"+topic+", 创建消费者失败:", consumerErr)
				return
			}
			partitionMap.Store(value, true)
			pc, err := consumer.ConsumePartition(topic, value, sarama.OffsetNewest)
			pc.IsPaused()
			if err != nil {
				log2.Logger.Error("创建消费者失败：    ", err)
				break
			}
			go ReceiveMessage(pc, partitionMap, value)

		}

	}

}

func ReceiveMessage(pc sarama.PartitionConsumer, partitionMap *sync.Map, partition int32) {
	defer pc.AsyncClose()
	defer partitionMap.Delete(partition)

	if pc == nil || partitionMap == nil {
		return
	}
	var requestTimeListMap = make(map[string]kao.RequestTimeList)
	var resultDataMsg = kao.ResultDataMsg{}
	var sceneTestResultDataMsg = new(kao.SceneTestResultDataMsg)
	var machineNum, startTime = int64(0), int64(0)
	var eventMap = make(map[string]int64)
	var machineMap = make(map[string]map[string]int64)
	log2.Logger.Info("分区：", partition, "   ,开始消费消息")
	for msg := range pc.Messages() {
		err := json.Unmarshal(msg.Value, &resultDataMsg)
		if err != nil {
			log2.Logger.Error("kafka消息转换失败：", err)
			continue
		}
		if resultDataMsg.ReportId == "" {
			log2.Logger.Error(fmt.Sprintf("es连接失败: %s", err))
			continue
		}

		if machineNum == 0 && resultDataMsg.MachineNum != 0 {
			machineNum = resultDataMsg.MachineNum + 1
		}

		if startTime == 0 {
			startTime = resultDataMsg.Timestamp
		}

		if resultDataMsg.Start {
			continue
		}

		if resultDataMsg.End {
			machineNum = machineNum - 1
			if machineNum == 1 {
				sceneTestResultDataMsg.End = true
				for eventId, requestTimeList := range requestTimeListMap {
					sort.Sort(requestTimeList)
					if sceneTestResultDataMsg.Results[eventId].TotalRequestNum != 0 {
						sceneTestResultDataMsg.Results[eventId].AvgRequestTime = float64(sceneTestResultDataMsg.Results[eventId].TotalRequestTime) / float64(sceneTestResultDataMsg.Results[eventId].TotalRequestNum)
					}
					sceneTestResultDataMsg.Results[eventId].MaxRequestTime = float64(requestTimeList[len(requestTimeList)-1])
					sceneTestResultDataMsg.Results[eventId].MinRequestTime = float64(requestTimeList[0])
					sceneTestResultDataMsg.Results[eventId].FiftyRequestTimeline = 50
					sceneTestResultDataMsg.Results[eventId].NinetyRequestTimeLine = 90
					sceneTestResultDataMsg.Results[eventId].NinetyFiveRequestTimeLine = 95
					sceneTestResultDataMsg.Results[eventId].NinetyNineRequestTimeLine = 99
					if sceneTestResultDataMsg.Results[eventId].PercentAge > 0 && sceneTestResultDataMsg.Results[eventId].PercentAge != 101 &&
						sceneTestResultDataMsg.Results[eventId].PercentAge != 50 && sceneTestResultDataMsg.Results[eventId].PercentAge != 90 &&
						sceneTestResultDataMsg.Results[eventId].PercentAge != 95 && sceneTestResultDataMsg.Results[eventId].PercentAge != 99 &&
						sceneTestResultDataMsg.Results[eventId].PercentAge != 100 {

						sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLineValue = kao.TimeLineCalculate(sceneTestResultDataMsg.Results[eventId].PercentAge, requestTimeList)
					}
					sceneTestResultDataMsg.Results[eventId].FiftyRequestTimelineValue = float64(requestTimeList[len(requestTimeList)/2])
					sceneTestResultDataMsg.Results[eventId].NinetyRequestTimeLineValue = kao.TimeLineCalculate(90, requestTimeList)
					sceneTestResultDataMsg.Results[eventId].NinetyFiveRequestTimeLineValue = kao.TimeLineCalculate(95, requestTimeList)
					sceneTestResultDataMsg.Results[eventId].NinetyNineRequestTimeLineValue = kao.TimeLineCalculate(99, requestTimeList)
					if sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLine != 0 {
						sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLineValue = kao.TimeLineCalculate(sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLine, requestTimeList)
					}
					if sceneTestResultDataMsg.Results[eventId].TotalRequestTime != 0 {
						concurrent := sceneTestResultDataMsg.Results[eventId].Concurrency
						sceneTestResultDataMsg.Results[eventId].Qps, _ = decimal.NewFromFloat(float64(sceneTestResultDataMsg.Results[eventId].TotalRequestNum) * float64(time.Second) * float64(concurrent) / float64(sceneTestResultDataMsg.Results[eventId].TotalRequestTime)).Round(2).Float64()
						sceneTestResultDataMsg.Results[eventId].SRps, _ = decimal.NewFromFloat(float64(sceneTestResultDataMsg.Results[eventId].SuccessNum) * float64(time.Second) * float64(concurrent) / float64(sceneTestResultDataMsg.Results[eventId].TotalRequestTime)).Round(2).Float64()
					}

				}
				sceneTestResultDataMsg.TimeStamp = resultDataMsg.Timestamp / 1000
				if err = redis.InsertTestData(machineMap, sceneTestResultDataMsg); err != nil {
					log2.Logger.Error("redis写入数据失败:", err)
				}
				if err = redis.UpdatePartitionStatus(conf.Conf.Kafka.Key, partition); err != nil {
					log2.Logger.Error("修改kafka分区状态失败： ", err)
				}
				log2.Logger.Info("删除key：", conf.Conf.Kafka.Key, " 的值：  ", partition, "成功")
				return
			}
			continue
		}
		if sceneTestResultDataMsg.SceneId == 0 {
			sceneTestResultDataMsg.SceneId = resultDataMsg.SceneId
		}
		if sceneTestResultDataMsg.SceneName == "" {
			sceneTestResultDataMsg.SceneName = resultDataMsg.SceneName
		}
		if sceneTestResultDataMsg.ReportId == "" {
			sceneTestResultDataMsg.ReportId = resultDataMsg.ReportId
		}
		if sceneTestResultDataMsg.ReportName == "" {
			sceneTestResultDataMsg.ReportName = resultDataMsg.ReportName
		}
		if sceneTestResultDataMsg.PlanId == 0 {
			sceneTestResultDataMsg.PlanId = resultDataMsg.PlanId
		}
		if sceneTestResultDataMsg.PlanName == "" {
			sceneTestResultDataMsg.PlanName = resultDataMsg.PlanName
		}
		if sceneTestResultDataMsg.Results == nil {
			sceneTestResultDataMsg.Results = make(map[string]*kao.ApiTestResultDataMsg)
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId] == nil {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId] = new(kao.ApiTestResultDataMsg)
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId].EventId == "" {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].EventId = resultDataMsg.EventId
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId].Name == "" {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].Name = resultDataMsg.Name
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId].SceneId == 0 {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].SceneId = resultDataMsg.SceneId
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId].PlanId == 0 {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].PlanId = resultDataMsg.PlanId
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId].PlanName == "" {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].PlanName = resultDataMsg.PlanName
		}
		if sceneTestResultDataMsg.Results[resultDataMsg.EventId].SceneName == "" {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].SceneName = resultDataMsg.SceneName
		}
		if resultDataMsg.PercentAge != 0 && resultDataMsg.PercentAge < 100 {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].CustomRequestTimeLine = resultDataMsg.PercentAge
		} else {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].CustomRequestTimeLine = 0
		}

		if concurrency, ok := machineMap[resultDataMsg.MachineIp][resultDataMsg.EventId]; !ok {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].Concurrency += resultDataMsg.Concurrency
			eventMap[resultDataMsg.EventId] = resultDataMsg.Concurrency
			machineMap[resultDataMsg.MachineIp] = eventMap
		} else {
			if concurrency != resultDataMsg.Concurrency {
				machineMap[resultDataMsg.MachineIp][resultDataMsg.EventId] = resultDataMsg.Concurrency
				sceneTestResultDataMsg.Results[resultDataMsg.EventId].Concurrency += resultDataMsg.Concurrency - concurrency
			}

		}
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].PercentAge = resultDataMsg.PercentAge
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].ErrorThreshold = resultDataMsg.ErrorThreshold
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].ResponseThreshold = resultDataMsg.ResponseThreshold
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].RequestThreshold = resultDataMsg.RequestThreshold
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].ReceivedBytes += resultDataMsg.ReceivedBytes
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].SendBytes += resultDataMsg.SendBytes
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].TotalRequestNum += 1
		sceneTestResultDataMsg.Results[resultDataMsg.EventId].TotalRequestTime += resultDataMsg.RequestTime
		if resultDataMsg.IsSucceed {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].SuccessNum += 1
		} else {
			sceneTestResultDataMsg.Results[resultDataMsg.EventId].ErrorNum += 1
		}
		requestTimeListMap[resultDataMsg.EventId] = append(requestTimeListMap[resultDataMsg.EventId], resultDataMsg.RequestTime)
		if resultDataMsg.Timestamp-startTime >= 1000 {
			startTime = resultDataMsg.Timestamp
			if sceneTestResultDataMsg.ReportId == "" || sceneTestResultDataMsg.Results == nil {
				break
			}
			for eventId, requestTimeList := range requestTimeListMap {
				sort.Sort(requestTimeList)
				if sceneTestResultDataMsg.Results[eventId].TotalRequestNum != 0 {
					sceneTestResultDataMsg.Results[eventId].AvgRequestTime = float64(sceneTestResultDataMsg.Results[eventId].TotalRequestTime) / float64(sceneTestResultDataMsg.Results[eventId].TotalRequestNum)
				}

				sceneTestResultDataMsg.Results[eventId].MaxRequestTime = float64(requestTimeList[len(requestTimeList)-1])
				sceneTestResultDataMsg.Results[eventId].MinRequestTime = float64(requestTimeList[0])
				sceneTestResultDataMsg.Results[eventId].FiftyRequestTimeline = 50
				sceneTestResultDataMsg.Results[eventId].NinetyRequestTimeLine = 90
				sceneTestResultDataMsg.Results[eventId].NinetyFiveRequestTimeLine = 95
				sceneTestResultDataMsg.Results[eventId].NinetyNineRequestTimeLine = 99
				if sceneTestResultDataMsg.Results[eventId].PercentAge > 0 && sceneTestResultDataMsg.Results[eventId].PercentAge != 101 &&
					sceneTestResultDataMsg.Results[eventId].PercentAge != 50 && sceneTestResultDataMsg.Results[eventId].PercentAge != 90 &&
					sceneTestResultDataMsg.Results[eventId].PercentAge != 95 && sceneTestResultDataMsg.Results[eventId].PercentAge != 99 &&
					sceneTestResultDataMsg.Results[eventId].PercentAge != 100 {

					sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLineValue = kao.TimeLineCalculate(sceneTestResultDataMsg.Results[eventId].PercentAge, requestTimeList)
				}
				sceneTestResultDataMsg.Results[eventId].FiftyRequestTimelineValue = float64(requestTimeList[len(requestTimeList)/2])
				sceneTestResultDataMsg.Results[eventId].NinetyRequestTimeLineValue = kao.TimeLineCalculate(90, requestTimeList)
				sceneTestResultDataMsg.Results[eventId].NinetyFiveRequestTimeLineValue = kao.TimeLineCalculate(95, requestTimeList)
				sceneTestResultDataMsg.Results[eventId].NinetyNineRequestTimeLineValue = kao.TimeLineCalculate(99, requestTimeList)
				if sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLine != 0 {
					sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLineValue = kao.TimeLineCalculate(sceneTestResultDataMsg.Results[eventId].CustomRequestTimeLine, requestTimeList)
				}
				if sceneTestResultDataMsg.Results[eventId].TotalRequestTime != 0 {
					concurrent := sceneTestResultDataMsg.Results[eventId].Concurrency
					sceneTestResultDataMsg.Results[eventId].Qps, _ = decimal.NewFromFloat(float64(sceneTestResultDataMsg.Results[eventId].TotalRequestNum) * float64(time.Second) * float64(concurrent) / float64(sceneTestResultDataMsg.Results[eventId].TotalRequestTime)).Round(2).Float64()
					sceneTestResultDataMsg.Results[eventId].SRps, _ = decimal.NewFromFloat(float64(sceneTestResultDataMsg.Results[eventId].SuccessNum) * float64(time.Second) * float64(concurrent) / float64(sceneTestResultDataMsg.Results[eventId].TotalRequestTime)).Round(2).Float64()
				}
				sceneTestResultDataMsg.TimeStamp = startTime / 1000
			}
			if err = redis.InsertTestData(machineMap, sceneTestResultDataMsg); err != nil {
				log2.Logger.Error("测试数据写入redis失败：     ", err)
				continue
			}
		}

	}
}
