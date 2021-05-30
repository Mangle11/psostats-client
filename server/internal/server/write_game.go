package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/phelix-/psostats/v2/pkg/model"
	"github.com/phelix-/psostats/v2/server/internal/db"
	"log"
	"time"
)

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
	questRun.UserName = user
	gameId, err := db.WriteGameById(&questRun, s.dynamoClient)
	if err != nil {
		log.Printf("write game %v", err)
		c.Status(500)
		return err
	}
	questRun.Id = gameId

	var matchingGame *model.QuestRun = nil
	for _, recentGame := range s.recentGames {
		if gamesMatch(recentGame, questRun) {
			log.Printf("game[%v] matched game[%v]", questRun.Id, recentGame.Id)
			matchingGame = &recentGame
			break
		}
	}

	if matchingGame != nil {
		err := db.AttachGameToId(questRun, matchingGame.Id, s.dynamoClient)
		if err != nil {
			log.Printf("%v", err)
		}
	} else {
		s.recentGames[s.recentGamesCount%s.recentGamesSize] = questRun
		s.recentGamesCount++
	}

	if isLeaderboardCandidate(questRun) {
		numPlayers := len(questRun.AllPlayers)
		if matchingGame == nil {
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
		gameId, questRun.QuestName, questRun.PlayerName, questRun.Server, questRun.UserName)
	return nil
}