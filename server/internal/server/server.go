package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/phelix-/psostats/v2/server/internal/db"
	"github.com/phelix-/psostats/v2/server/internal/userdb"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/gofiber/fiber/v2"
	"github.com/phelix-/psostats/v2/pkg/model"
)

type Server struct {
	app              *fiber.App
	dynamoClient     *dynamodb.DynamoDB
	userDb           userdb.UserDb
	recentGames      []model.QuestRun
	recentGamesCount int
	recentGamesSize  int
}

func New(dynamo *dynamodb.DynamoDB) *Server {
	f := fiber.New(fiber.Config{
		// modify config
	})
	cacheSize := 500
	return &Server{
		app:              f,
		dynamoClient:     dynamo,
		userDb:           userdb.DynamoInstance(dynamo),
		recentGames:      make([]model.QuestRun, cacheSize),
		recentGamesCount: 0,
		recentGamesSize:  cacheSize,
	}
}

func (s *Server) Run() {
	s.app.Static("/main.css", "./static/main.css", fiber.Static{})
	s.app.Static("/favicon.ico", "./static/favicon.ico", fiber.Static{})
	s.app.Static("/static/", "./static/", fiber.Static{})
	// UI
	s.app.Get("/", s.Index)
	s.app.Get("/game/:gameId", s.GamePage)
	s.app.Get("/records", s.RecordsPage)
	s.app.Get("/players/:player", s.PlayerPage)
	s.app.Get("/gc/:gc", s.GcRedirect)
	// API
	s.app.Post("/api/game", s.PostGame)
	s.app.Get("/api/game/:gameId", s.GetGame)

	if certLocation, found := os.LookupEnv("CERT"); found {
		keyLocation := os.Getenv("KEY")
		if err := s.app.ListenTLS(":443", certLocation, keyLocation); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := s.app.Listen(":80"); err != nil {
			log.Fatal(err)
		}
	}
}

func (s *Server) Index(c *fiber.Ctx) error {
	t, err := template.ParseFiles("./server/internal/templates/index.gohtml")
	if err != nil {
		c.Status(500)
		return err
	}
	games, err := db.GetRecentGames(s.dynamoClient)
	if err != nil {
		log.Printf("get recent games %v", err)
		c.Status(500)
		return err
	}
	for i, game := range games {
		addFormattedFields(&game)
		games[i] = game
	}
	model := struct {
		Games []model.Game
	}{
		Games: games,
	}
	c.Response().Header.Set("Content-Type", "text/html; charset=UTF-8")
	err = t.ExecuteTemplate(c.Response().BodyWriter(), "index", model)
	return err
}

func (s *Server) GamePage(c *fiber.Ctx) error {
	gameId := c.Params("gameId")
	game, err := db.GetGame(gameId, s.dynamoClient)
	if err != nil {
		return err
	}

	if game == nil {
		t, err := template.ParseFiles("./server/internal/templates/gameNotFound.gohtml")
		if err != nil {
			return err
		}
		err = t.ExecuteTemplate(c.Response().BodyWriter(), "gameNotFound", game)
	} else {
		invincibleRanges := make(map[int]int)
		invincibleStart := -1
		for i, invincible := range game.Invincible {
			if invincible {
				if invincibleStart < 0 {
					invincibleStart = i
				}
			} else {
				if invincibleStart > 0 {
					if i-invincibleStart >= 10 {
						invincibleRanges[invincibleStart] = i
					}
					invincibleStart = -1
				}
			}
		}
		model := struct {
			Game                 model.QuestRun
			InvincibleRanges     map[int]int
			HpRanges             map[int]uint16
			TpRanges             map[int]uint16
			MonstersAliveRanges  map[int]int
			MonstersKilledRanges map[int]int
			MesetaChargedRanges  map[int]int
			FreezeTrapRanges     map[int]uint16
			ShiftaRanges         map[int]int16
			DebandRanges         map[int]int16
			HpPoolRanges         map[int]int
		}{
			Game:                 *game,
			InvincibleRanges:     invincibleRanges,
			HpRanges:             convertU16ToXY(game.HP),
			TpRanges:             convertU16ToXY(game.TP),
			MonstersAliveRanges:  convertIntToXY(game.MonsterCount),
			MonstersKilledRanges: convertIntToXY(game.MonstersKilledCount),
			MesetaChargedRanges:  convertIntToXY(game.MesetaCharged),
			FreezeTrapRanges:     convertU16ToXY(game.FreezeTraps),
			ShiftaRanges:         convertToXY(game.ShiftaLvl),
			DebandRanges:         convertToXY(game.DebandLvl),
			HpPoolRanges:         convertIntToXY(game.MonsterHpPool),
		}
		t, err := template.ParseFiles("./server/internal/templates/game.gohtml")
		if err != nil {
			return err
		}
		err = t.ExecuteTemplate(c.Response().BodyWriter(), "game", model)
	}
	c.Response().Header.Set("Content-Type", "text/html; charset=UTF-8")
	return err
}

func convertIntToXY(values []int) map[int]int {
	converted := make(map[int]int)
	previousValue := 0
	for i, value := range values {
		if i == 0 || value != previousValue {
			converted[i] = value
			previousValue = value
		}
	}
	converted[len(values)-1] = previousValue
	return converted
}

func convertU16ToXY(values []uint16) map[int]uint16 {
	converted := make(map[int]uint16)
	previousValue := uint16(0)
	for i, value := range values {
		if i == 0 || value != previousValue {
			converted[i] = value
			previousValue = value
		}
	}
	converted[len(values)-1] = previousValue
	return converted
}

func convertToXY(values []int16) map[int]int16 {
	converted := make(map[int]int16)
	previousValue := int16(0)
	for i, value := range values {
		if i == 0 || value != previousValue {
			converted[i] = value
			previousValue = value
		}
	}
	converted[len(values)-1] = previousValue
	return converted
}

func (s *Server) RecordsPage(c *fiber.Ctx) error {
	t, err := template.ParseFiles("./server/internal/templates/records.gohtml")
	if err != nil {
		c.Status(500)
		return err
	}
	games, err := db.GetQuestRecords(s.dynamoClient)
	sort.Slice(games, func(i, j int) bool {
		if games[i].Episode != games[j].Episode {
			return games[i].Episode < games[j].Episode
		}
		if games[i].Quest != games[j].Quest {
			return games[i].Quest < games[j].Quest
		}
		return games[i].Category < games[j].Category
	})

	if err != nil {
		log.Print("get recent games")
		c.Status(500)
		return err
	}
	for i, game := range games {
		addFormattedFields(&game)
		games[i] = game
	}
	model := struct {
		Games []model.Game
	}{
		Games: games,
	}
	c.Response().Header.Set("Content-Type", "text/html; charset=UTF-8")
	err = t.ExecuteTemplate(c.Response().BodyWriter(), "index", model)
	return err
}

func addFormattedFields(game *model.Game) {
	minutes := game.Time / time.Minute
	seconds := (game.Time % time.Minute) / time.Second
	game.FormattedTime = fmt.Sprintf("%01d:%02d", minutes, seconds)
	shortCategory := game.Category
	numPlayers := string(shortCategory[0])
	pbRun := string(shortCategory[1])
	pbText := ""
	if pbRun == "p" {
		pbText = " PB"
	}
	game.Category = numPlayers + " Player" + pbText
	location, err := time.LoadLocation("America/Chicago")
	if err != nil {
		log.Fatalf("Couldn't find time zone America/Chicago")
	}
	game.FormattedDate = game.Timestamp.In(location).Format("15:04 01/02/2006")
}

func (s *Server) PlayerPage(c *fiber.Ctx) error {
	player := c.Params("player")
	t, err := template.ParseFiles("./server/internal/templates/player.gohtml")
	if err != nil {
		c.Status(500)
		return err
	}

	player, err = url.PathUnescape(player)
	if err != nil {
		c.Status(500)
		return err
	}
	pbs, err := db.GetPlayerPbs(player, s.dynamoClient)

	if err != nil {
		log.Print("get pbs")
		c.Status(500)
		return err
	}
	sort.Slice(pbs, func(i, j int) bool { return pbs[i].Quest < pbs[j].Quest })
	for i, game := range pbs {
		addFormattedFields(&game)
		pbs[i] = game
	}

	recent, err := db.GetPlayerRecentGames(player, s.dynamoClient)

	if err != nil {
		log.Print("get recent")
		c.Status(500)
		return err
	}
	for i, game := range recent {
		addFormattedFields(&game)
		recent[i] = game
	}

	model := struct {
		Player      string
		PlayerPbs   []model.Game
		RecentGames []model.Game
	}{
		Player:      player,
		PlayerPbs:   pbs,
		RecentGames: recent,
	}
	c.Response().Header.Set("Content-Type", "text/html; charset=UTF-8")
	err = t.ExecuteTemplate(c.Response().BodyWriter(), "index", model)
	return err
}

func (s *Server) GcRedirect(c *fiber.Ctx) error {
	gc := c.Params("gc")
	playerName, err := s.userDb.GetUsernameByGc(gc)
	if err != nil {
		log.Printf("loading player by gc %v %v", gc, err)
	}
	return c.Redirect(fmt.Sprintf("/players/%v", playerName))
}

func (s *Server) PostGame(c *fiber.Ctx) error {
	user, pass, err := s.getUserFromBasicAuth(c.Request().Header.Peek("Authorization"))
	if err != nil {
		c.Status(401)
		return nil
	}
	userObject, err := s.userDb.GetUser(user)
	if err != nil {
		c.Status(401)
		return nil
	}
	if passwordsMatch := DoPasswordsMatch(userObject.Password, pass); !passwordsMatch {
		c.Status(401)
		return nil
	}
	var questRun model.QuestRun
	if err := c.BodyParser(&questRun); err != nil {
		log.Printf("body parser")
		c.Status(400)
		return err
	}
	questDuration, err := time.ParseDuration(questRun.QuestDuration)
	if err != nil {
		c.Status(400)
		return err
	}
	questRun.GuildCard = user
	gameId, err := db.WriteGameById(&questRun, s.dynamoClient)
	if err != nil {
		log.Printf("write game %v", err)
		c.Status(500)
		return err
	}
	questRun.Id = gameId

	for _, recentGame := range s.recentGames {
		if gamesMatch(recentGame, questRun) {
			log.Printf("game[%v] matched game[%v]", questRun.Id, recentGame.Id)
		}
	}
	s.recentGames[s.recentGamesCount%s.recentGamesSize] = questRun
	s.recentGamesCount++

	if isLeaderboardCandidate(questRun) {
		numPlayers := len(questRun.AllPlayers)
		topRun, err := db.GetQuestRecord(questRun.QuestName, numPlayers, questRun.PbCategory, s.dynamoClient)
		if err != nil {
			log.Printf("failed to get top quest runs for gameId:%v - %v", gameId, err)
		} else if topRun == nil || topRun.Time > questDuration {
			log.Printf("new record for %v %vp pb:%v - %v",
				questRun.QuestName, numPlayers, questRun.PbCategory, gameId)
			if err = db.WriteGameByQuestRecord(&questRun, s.dynamoClient); err != nil {
				log.Printf("failed to update leaderboard for game %v - %v", gameId, err)
			}
		}
		if err = db.WriteGameByQuest(&questRun, s.dynamoClient); err != nil {
			log.Printf("failed to update games by quest for game %v - %v", gameId, err)
		}
		playerPb, err := db.GetPlayerPB(questRun.QuestName, user, numPlayers, questRun.PbCategory, s.dynamoClient)
		if err != nil {
			log.Printf("failed to get player pb for gameId:%v - %v", gameId, err)
		} else if playerPb == nil || playerPb.Time > questDuration {
			log.Printf("new pb for %v %v %vp pb:%v - %v",
				user, questRun.QuestName, numPlayers, questRun.PbCategory, gameId)
			if err = db.WritePlayerPb(&questRun, s.dynamoClient); err != nil {
				log.Printf("failed to update pb for game %v - %v", gameId, err)
			}
		}
	}
	if err = db.WriteGameByPlayer(&questRun, s.dynamoClient); err != nil {
		log.Printf("failed to update games by player for game %v - %v", gameId, err)
	}

	c.Response().AppendBodyString(gameId)
	log.Printf("got quest: %v %v, %v, %v, %v",
		gameId, questRun.QuestName, questRun.PlayerName, questRun.Server, questRun.GuildCard)
	return nil
}

func (s *Server) GetGame(c *fiber.Ctx) error {
	gameId := c.Params("gameId")
	game, err := db.GetGame(gameId, s.dynamoClient)
	if err != nil {
		return err
	}

	if game == nil {
		c.Status(404)
		return nil
	} else {
		jsonBytes, err := json.Marshal(game)
		if err != nil {
			return err
		}
		c.Response().AppendBody(jsonBytes)
		c.Response().Header.Set("Content-Type", "application/json")
		return nil
	}
}

func isLeaderboardCandidate(questRun model.QuestRun) bool {
	allowedDifficulty := questRun.Difficulty == "Ultimate" || strings.HasPrefix(questRun.QuestName, "Stage")
	return allowedDifficulty && questRun.QuestComplete && !questRun.IllegalShifta
}

func (s *Server) getUserFromBasicAuth(headerBytes []byte) (string, string, error) {
	headerString := string(headerBytes)
	if len(headerString) > 0 && strings.HasPrefix(headerString, "Basic ") {
		authBase64 := strings.TrimPrefix(headerString, "Basic ")
		decoded, err := base64.StdEncoding.DecodeString(authBase64)
		if err != nil {
			return "", "", err
		}
		auth := string(decoded)
		authSplit := strings.SplitN(auth, ":", 2)

		return authSplit[0], authSplit[1], nil
	} else {
		return "", "", errors.New("missing basic auth header")
	}
}

func gamesMatch(a, b model.QuestRun) bool {
	if a.QuestName != b.QuestName {
		return false
	}
	if a.Difficulty != b.Difficulty {
		return false
	}
	if a.Episode != b.Episode {
		return false
	}
	if a.Server != b.Server {
		return false
	}
	if a.GuildCard == b.GuildCard {
		return false
	}
	if a.QuestStartTime.Add(time.Second*-30).After(b.QuestStartTime) &&
		a.QuestStartTime.Add(time.Second*30).Before(b.QuestStartTime) {
		return false
	}
	if a.QuestEndTime.Add(time.Second*-30).After(b.QuestEndTime) &&
		a.QuestEndTime.Add(time.Second*30).Before(b.QuestEndTime) {
		return false
	}
	if len(a.AllPlayers) != len(b.AllPlayers) {
		return false
	}
	for i := range a.AllPlayers {
		if a.AllPlayers[i] != b.AllPlayers[i] {
			return false
		}
	}
	return true
}
