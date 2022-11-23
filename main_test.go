package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gamezop/interview-assignment-sc-rewards/repo"
	"github.com/gamezop/jinbe/pkg/db_psql"
	"github.com/gamezop/jinbe/pkg/errr"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func wireApp(testClient *http.Client) (*gin.Engine, *repo.Queries, func()) {
	err := godotenv.Load("scratch-cards-rewards.e2e-test.env")
	errr.FatalIfNotNil(err, "failed to load e2e env")
	err = envconfig.Process("", &Env)
	errr.PanicIfNotNil(err, "failed to load env")

	rand.Seed(time.Now().Unix())

	dbRewards := db_psql.DBConnectURL("test-db", Env.DB.RewardPayoutsURI)
	runMigrate(dbRewards)

	repository := repo.New(dbRewards)

	return Router(repository, testClient), repository, func() {
		dbRewards.Close()
	}
}

func makePayoutRewardAPICall(t *testing.T, r *gin.Engine, path string, ScId uuid.UUID, headers map[string]string) ResponseRewardPayout {
	body := RequestBodyRewardPayout{
		ScId: ScId,
	}
	bodyStr, err := json.Marshal(body)
	require.Nil(t, err, "failed to marshal")
	req, err := http.NewRequest("POST", path, strings.NewReader(string(bodyStr)))
	require.Nil(t, err, "failed to create request")
	for k, v := range headers {
		req.Header.Add(k, v)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	responseData, err := ioutil.ReadAll(w.Body)
	require.Nil(t, err, "failed to read response")

	var response struct {
		Data ResponseRewardPayout `json:"data"`
	}
	err = json.Unmarshal(responseData, &response)
	require.Nil(t, err, "failed to unmarshal response")

	return response.Data
}

func TestPayoutRewardR1(t *testing.T) {
	r, repo, close := wireApp(&http.Client{})
	ctxBack := context.Background()
	defer close()

	ScId := uuid.New()
	response := makePayoutRewardAPICall(t, r, "/r1/payout", ScId, nil)

	// test data
	rewardPayoutDetails, err := repo.GetRewardPayoutByScratchId(ctxBack, ScId)
	require.Nil(t, err, "failed read scCard DB")

	require.Equal(t, rewardPayoutDetails.OrderID, response.OrderId, "failed to match orderId", rewardPayoutDetails.OrderID.String(), response.OrderId.String())
	require.Equal(t, rewardPayoutDetails.Status, response.Status, "failed to match status")
}

func TestPayoutRewardR2(t *testing.T) {
	r, repository, close := wireApp(&http.Client{})
	ctxBack := context.Background()
	defer close()

	ScId := uuid.New()
	response := makePayoutRewardAPICall(t, r, "/r2/payout", ScId, nil)

	// initial require the status to be in pending
	rewardPayoutDetails, err := repository.GetRewardPayoutByScratchId(ctxBack, ScId)
	require.Nil(t, err, "expected no error")
	require.Equal(t, repo.OrderStatusPending, response.Status, "require the r2 to be in pending initially")
	require.Equal(t, rewardPayoutDetails.OrderID, response.OrderId, "failed to match orderId")

	time.Sleep(time.Duration(maxOrderStatusInPendingStateSeconds+1) * time.Second)

	rewardPayoutDetails, err = repository.GetRewardPayoutByScratchId(ctxBack, ScId)
	require.Nil(t, err, "failed read scCard DB")
	require.NotEqual(t, rewardPayoutDetails.Status, repo.OrderStatusPending, "didn't expect the order to be in pending")
	require.Equal(t, rewardPayoutDetails.ScID, ScId, "failed to match sc-card-id")
	require.NotEqual(t, rewardPayoutDetails.Status, repo.OrderStatusPending, "didn't expect the order to be in pending")
}

func TestPayoutRewardR3(t *testing.T) {
	defer gock.Off()
	var testClient *http.Client = &http.Client{}
	r, repository, close := wireApp(testClient)
	gock.InterceptClient(testClient)
	ctxBack := context.Background()
	defer close()

	ScId := uuid.New()
	mockServerAddr := "https://local.testing"
	mockServerPath := "/completedOrder"
	response := makePayoutRewardAPICall(t, r, "/r3/payout", ScId, map[string]string{
		"x-callback-url": mockServerAddr + mockServerPath,
	})

	// initial require the status to be in pending
	rewardPayoutDetails, err := repository.GetRewardPayoutByScratchId(ctxBack, ScId)
	require.Nil(t, err, "expected no error")
	require.Equal(t, repo.OrderStatusPending, response.Status, "require the r2 to be in pending initially")
	require.Equal(t, rewardPayoutDetails.OrderID, response.OrderId, "failed to match orderId")

	// expecting a callback on the api
	gock.New(mockServerAddr).
		Put(mockServerPath).
		Times(1).
		Reply(http.StatusOK).
		BodyString(`{"status": "SUCCESS"}`)

	time.Sleep(time.Duration(maxOrderStatusInPendingStateSeconds+1) * time.Second)

	rewardPayoutDetails, err = repository.GetRewardPayoutByScratchId(ctxBack, ScId)
	require.Nil(t, err, "failed read scCard DB")
	require.Equal(t, rewardPayoutDetails.ScID, ScId, "failed to match sc-card-id")
	require.NotEqual(t, rewardPayoutDetails.Status, repo.OrderStatusPending, "didn't expect the order to be in pending")
}
