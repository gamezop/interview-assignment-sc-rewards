package main

import (
	"database/sql"
	"errors"
	"math/rand"
	"net/http"
	"time"

	"github.com/gamezop/interview-assignment-sc-rewards/repo"
	"github.com/gamezop/jinbe/pkg/db_psql"
	"github.com/gamezop/jinbe/pkg/errr"
	"github.com/gamezop/jinbe/pkg/http/ginhelpers"
	gh "github.com/gamezop/jinbe/pkg/http/ginhelpers"
	"github.com/gamezop/jinbe/pkg/http/httpclient"
	"github.com/gamezop/jinbe/pkg/random"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog/log"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

var Version = "unknown-updated-during-build-time"
var serviceName = "reward-service-mock"
var maxOrderStatusInPendingStateSeconds = 10

func runMigrate(db *sql.DB) {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	errr.PanicIfNotNil(err, "failed to get driver migrate")
	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"postgres", driver)
	errr.PanicIfNotNil(err, "failed to get migration instance")
	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		errr.PanicIfNotNil(err, "failed to run migration")
	}
	log.Info().Msg("completed migration")
}

//////// config
type config struct {
	Flags struct {
		OnlySuccessfullyOrders bool `split_words:"true"`
	}
	DB struct {
		RewardPayoutsURI string `required:"true" split_words:"true"`
	}
}

var Env config

func Init() {
	rand.Seed(time.Now().Unix())

	err := envconfig.Process("", &Env)
	errr.PanicIfNotNil(err, "failed to load env")
}

//////// random logic

func NewOrderId() uuid.UUID {
	return uuid.New()
}
func getRandomOrderStatus(arr []repo.OrderStatus) repo.OrderStatus {
	return arr[rand.Intn(len(arr))]
}

func getTerminalOrderStatus() repo.OrderStatus {
	var status repo.OrderStatus = getRandomOrderStatus([]repo.OrderStatus{
		repo.OrderStatusFailed,
		repo.OrderStatusSuccess,
	})

	if Env.Flags.OnlySuccessfullyOrders {
		return repo.OrderStatusSuccess
	}
	return status
}

//////// handlers

type RequestBodyRewardPayout struct {
	ScId uuid.UUID `json:"scId" binding:"required"`
}

type ResponseRewardPayout struct {
	Status  repo.OrderStatus `json:"status"`
	OrderId uuid.UUID        `json:"orderId"`
}

func handlerR1Payout(
	gM *gh.GinResponseHelper,
	r *repo.Queries,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := ginhelpers.GetDDCtxFromGinOrBackground(c)
		l := gh.RequestLogger(c)
		var req RequestBodyRewardPayout
		if err := c.ShouldBindJSON(&req); errr.IfErrAndLog(err, &l, "validation") {
			gM.RespondWithValidationError(c, err)
			return
		}

		rewardPayoutDetails, err := r.CreateRewardPayout(ctx, repo.CreateRewardPayoutParams{
			Status: getTerminalOrderStatus(),
			ScID:   req.ScId,
		})

		if errr.IfErrAndLog(err, &l, "failed to create reward payout") {
			gM.RespondWithError(c, "DB", err)
			return
		}

		gM.RespondWithSuccess(c, ResponseRewardPayout{
			Status:  rewardPayoutDetails.Status,
			OrderId: rewardPayoutDetails.OrderID,
		})
		return
	}
}

func handlerR2Payout(
	gM *gh.GinResponseHelper,
	r *repo.Queries,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := ginhelpers.GetDDCtxFromGinOrBackground(c)
		l := gh.RequestLogger(c)
		var req RequestBodyRewardPayout
		if err := c.ShouldBindJSON(&req); errr.IfErrAndLog(err, &l, "validation") {
			gM.RespondWithValidationError(c, err)
			return
		}

		rewardPayoutDetails, err := r.CreateRewardPayout(ctx, repo.CreateRewardPayoutParams{
			Status: repo.OrderStatusPending,
			ScID:   req.ScId,
		})

		if errr.IfErrAndLog(err, &l, "failed to create reward payout") {
			gM.RespondWithError(c, "DB", err)
			return
		}

		go func() {
			time.Sleep(time.Duration(random.RandInt(2, maxOrderStatusInPendingStateSeconds)) * time.Second)

			terminalStatus := getTerminalOrderStatus()
			l = l.With().
				Str("terminalStatus", string(terminalStatus)).
				Str("orderId", rewardPayoutDetails.OrderID.String()).
				Logger()
			err := r.UpdateRewardPayoutStatus(ctx, repo.UpdateRewardPayoutStatusParams{Status: terminalStatus, OrderID: rewardPayoutDetails.OrderID})
			errr.LogIfErr(err, &l, "failed to update reward payout status")
			l.Trace().Msg("updated order status")
		}()

		gM.RespondWithSuccess(c, ResponseRewardPayout{
			Status:  rewardPayoutDetails.Status,
			OrderId: rewardPayoutDetails.OrderID,
		})
		return
	}
}

type RequestQueryParamsRewardPayout2Status struct {
	OrderId string `form:"order-id"`
}

type ResponseRewardPayouts2Status struct {
	Status repo.OrderStatus `json:"status"`
}

func handlerR2Status(
	gM *gh.GinResponseHelper,
	r *repo.Queries,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := ginhelpers.GetDDCtxFromGinOrBackground(c)
		l := gh.RequestLogger(c)
		var req RequestQueryParamsRewardPayout2Status
		if err := c.ShouldBindQuery(&req); errr.IfErrAndLog(err, &l, "validation") {
			gM.RespondWithValidationError(c, err)
			return
		}

		rewardPayoutDetails, err := r.GetRewardPayoutByOrderId(ctx, uuid.MustParse(req.OrderId))
		if errr.IfErrAndLog(err, &l, "failed to get reward payout") {
			gM.RespondWithError(c, "DB", err)
			return
		}

		gM.RespondWithSuccess(c, ResponseRewardPayouts2Status{
			Status: rewardPayoutDetails.Status,
		})
		return
	}
}

type RequestHeadersRewardPayout3 struct {
	CallbackURL string `header:"x-callback-url" binding:"required,url"`
}

func handlerR3Payout(
	gM *gh.GinResponseHelper,
	r *repo.Queries,
	webhookHttpClient *http.Client,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := ginhelpers.GetDDCtxFromGinOrBackground(c)
		l := gh.RequestLogger(c)
		var req RequestBodyRewardPayout
		if err := c.ShouldBindJSON(&req); errr.IfErrAndLog(err, &l, "validation") {
			gM.RespondWithValidationError(c, err)
			return
		}

		var reqHeaders RequestHeadersRewardPayout3
		if err := c.ShouldBindHeader(&reqHeaders); errr.IfErrAndLog(err, &l, "validation") {
			gM.RespondWithValidationError(c, err)
			return
		}

		rewardPayoutDetails, err := r.CreateRewardPayout(ctx, repo.CreateRewardPayoutParams{
			Status: repo.OrderStatusPending,
			ScID:   req.ScId,
		})

		if errr.IfErrAndLog(err, &l, "failed to create reward payout") {
			gM.RespondWithError(c, "DB", err)
			return
		}

		go func() {
			time.Sleep(time.Duration(random.RandInt(2, maxOrderStatusInPendingStateSeconds)) * time.Second)

			terminalStatus := getTerminalOrderStatus()
			l = l.With().
				Str("terminalStatus", string(terminalStatus)).
				Str("orderId", rewardPayoutDetails.OrderID.String()).
				Logger()
			err := r.UpdateRewardPayoutStatus(ctx, repo.UpdateRewardPayoutStatusParams{Status: terminalStatus, OrderID: rewardPayoutDetails.OrderID})
			errr.LogIfErr(err, &l, "failed to update reward payout status")

			newRewardPayoutDetails, err := r.GetRewardPayoutByOrderId(ctx, rewardPayoutDetails.OrderID)
			if errr.IfErrAndLog(err, &l, "failed to get reward payout after updating") {
				return
			}

			var response interface{}
			// ensure that a 2xx response is received
			_, err = httpclient.SendHttpRequest(ctx, webhookHttpClient, "PUT", reqHeaders.CallbackURL, nil, nil, newRewardPayoutDetails, &response)
			if errr.IfErrAndLog(err, &l, "failed to call webhook") {
				return
			}

			l.Trace().Msg("updated order status")
		}()

		gM.RespondWithSuccess(c, ResponseRewardPayout{
			Status:  rewardPayoutDetails.Status,
			OrderId: rewardPayoutDetails.OrderID,
		})
		return
	}
}

func Router(repository *repo.Queries, httpClient *http.Client) *gin.Engine {
	r := gin.New()
	ginModule := gh.NewGinResponseHelper(Version, serviceName, r, nil)
	r.Use(
		requestid.New(),
		gin.Logger(),
		gin.Recovery(),
	)
	r.POST("r1/payout", handlerR1Payout(ginModule, repository))
	r.POST("r2/payout", handlerR2Payout(ginModule, repository))
	// this api can be used to pull data for any reward type
	r.GET("r2/payout/status", handlerR2Status(ginModule, repository))
	r.POST("r3/payout", handlerR3Payout(ginModule, repository, httpClient))
	r.PUT("credit", func(c *gin.Context) {
		l := gh.RequestLogger(c)
		body, err := c.GetRawData()
		errr.LogIfErr(err, &l, "failed to get body")
		l.Info().Str("body", string(body)).Msg("credit api called")
	})

	return r
}

func main() {
	Init()
	DBRewards := db_psql.DBConnectUrl("reward", Env.DB.RewardPayoutsURI, serviceName, false)
	runMigrate(DBRewards)

	repository := repo.New(DBRewards)

	r := Router(repository, &http.Client{
		Timeout: 5 * time.Second,
	})
	err := r.Run(":3010")
	errr.PanicIfNotNil(err, "server stopped")
}
