package v1

import (
	"strconv"

	"github.com/chideat/glog"
	"github.com/chideat/pcc/action/models"
	. "github.com/chideat/pcc/action/modules/config"
	. "github.com/chideat/pcc/action/routes/utils"
	. "github.com/chideat/pcc/pig/models"
	"github.com/gin-gonic/gin"
	"github.com/golang/protobuf/proto"
	"github.com/nsqio/go-nsq"
)

var (
	producer *nsq.Producer
)

func init() {
	var err error

	config := nsq.NewConfig()
	producer, err = nsq.NewProducer(Conf.Queue.NsqdAddress, config)
	if err != nil {
		glog.Panic(err)
	}
	producer.SetLogger(nil, nsq.LogLevelError)
}

// Route: /feeds/:id/like/users
func GetFeedLikeUsers(c *gin.Context) {
	count, _ := strconv.Atoi(c.PostForm("count"))
	if count == 0 {
		count = 19
	}
	target, err := strconv.ParseInt(c.Params.ByName("id"), 10, 64)
	if err != nil {
		Json(c, "200001", "无效的参数")
		return
	}
	moodStr := c.DefaultPostForm("mood", c.Query("mood"))
	mood, _ := models.LikeMood_value[moodStr]

	actions, total, err := models.GetLikeActions(target, models.LikeMood(mood), count)
	if err != nil {
		Json(c, "100001", err.Error())
		return
	}
	rets := []interface{}{}
	for _, action := range actions {
		userInfo, err := action.UserInfo()
		if err != nil {
			glog.Error(err)
			continue
		}
		rets = append(rets, userInfo)
	}

	JsonWithDataInfo(c, "0", "OK", rets, map[string]interface{}{
		"total": total,
	})
}

// Route: /feeds/:id/like
// Method: POST
func FeedLike(c *gin.Context) {
	userId, err := strconv.ParseInt(c.DefaultPostForm("user_id", c.Query("user_id")), 10, 64)
	if err != nil || TYPE_USER&uint8(255&userId) == 0 {
		Json(c, "200001", "无效的用户ID")
		return
	}
	target, err := strconv.ParseInt(c.Params.ByName("id"), 10, 64)
	if err != nil {
		Json(c, "200001", "无效的参数")
		return
	}
	moodStr := c.DefaultPostForm("mood", c.Query("mood"))
	mood, _ := models.LikeMood_value[moodStr]
	if mood == 0 {
		mood = int32(models.LikeMood_like)
	}

	action, err := models.GetLikeActionByUserAndTarget(userId, target)
	if err != nil {
		Json(c, "100001", err.Error())
		return
	}
	method := models.RequestMethod_Add
	if action == nil {
		action, err = models.NewLikeAction(userId, target, models.LikeMood(mood))
		if err != nil {
			Json(c, "200001", err.Error())
			return
		}
	} else {
		// if no change, return directly
		if action.Mood == models.LikeMood(mood) && !action.Deleted {
			actionMap, err := action.Map()
			if err != nil {
				glog.Error(err)
			}
			JsonWithData(c, "0", "OK", actionMap)
			return
		}
		action.Mood = models.LikeMood(mood)

		method = models.RequestMethod_Update
	}

	// async add
	rawData, _ := proto.Marshal(&models.Request{Method: method, Data: action.Bytes()})
	err = producer.Publish("pcc.action_like", rawData)
	if err != nil {
		glog.Error(err)
		Json(c, "10001", err.Error())
		return
	}
	actionMap, err := action.Map()
	if err != nil {
		glog.Error(err)
	}
	JsonWithData(c, "0", "OK", actionMap)
}

// Route: /feeds/:id/like
// Method: DELETE
func FeedUnlike(c *gin.Context) {
	userId, err := strconv.ParseInt(c.DefaultPostForm("user_id", c.Query("user_id")), 10, 64)
	if err != nil || TYPE_USER&uint8(255&userId) == 0 {
		Json(c, "200001", "无效的用户ID")
		return
	}
	target, err := strconv.ParseInt(c.Params.ByName("id"), 10, 64)
	if err != nil {
		Json(c, "200001", "无效的参数")
		return
	}

	action, err := models.GetLikeActionByUserAndTarget(userId, target)
	if err != nil {
		Json(c, "100001", "取消点赞失败")
		return
	}
	if action == nil {
		Json(c, "100001", "已点过赞")
		return
	}
	if action.Deleted {
		Json(c, "100001", "未点赞")
		return
	}

	// async delete
	data, _ := proto.Marshal(&models.Request{Method: models.RequestMethod_Delete, Data: action.Bytes()})
	err = producer.Publish("pcc.action_like", data)
	if err != nil {
		glog.Error(err)
	}
	Json(c, "0", "OK")
}
