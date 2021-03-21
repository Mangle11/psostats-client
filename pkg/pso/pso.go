package pso

import (
	"fmt"
	"log"
	"syscall"
	"time"

	"github.com/TheTitanrain/w32"
	"github.com/phelix-/psostats/v2/pkg/pso/inventory"
	"github.com/phelix-/psostats/v2/pkg/pso/player"
)

const (
	unseenWindowName             = "PHANTASY STAR ONLINE Blue Burst"
	ephineaWindowName            = "Ephinea: Phantasy Star Online Blue Burst"
	persistentConnectionTickRate = time.Second / 30
	windowsCodeStillActive       = 259
	WarpIn                       = 0
	Switch                       = 1
)

type (
	handle w32.HANDLE
)

type PSO struct {
	questTypes        Quests
	connected         bool
	connectedStatus   string
	handle            handle
	CurrentPlayerData player.BasePlayerInfo
	EquippedWeapon    inventory.Weapon
	GameState         GameState
	CurrentQuest      int
	Quests            map[int]QuestRun
	errors            chan error
	done              chan struct{}
	MonsterNames      map[uint32]string
}

type GameState struct {
	MonsterCount      int
	FloorSwitches     bool
	QuestName         string
	QuestStarted      bool
	QuestComplete     bool
	QuestStartTime    time.Time
	QuestEndTime      time.Time
	monsterUnitxtAddr uint32
	Difficulty        string
	Episode           uint16
}

type PlayerData struct {
	CharacterName       string
	Class               string
	Guildcard           string
	HP                  uint16
	MaxHP               uint16
	TP                  uint16
	MaxTP               uint16
	Floor               uint16
	Room                uint16
	KillCount           uint16
	Meseta              uint32
	ShiftaLvl           int16
	DebandLvl           int16
	InvincibilityFrames uint32
	Time                time.Time
}

func New() *PSO {
	return &PSO{
		questTypes:   NewQuests(),
		Quests:       make(map[int]QuestRun),
		MonsterNames: make(map[uint32]string),
	}
}

func (pso *PSO) StartPersistentConnection(errors chan error) {
	if pso.done != nil {
		close(pso.done)
	}
	pso.done = make(chan struct{})
	go func() {
		for {
			select {
			case <-time.After(persistentConnectionTickRate):
				if !pso.connected {
					connected, connectedStatus, err := pso.Connect()
					pso.connectedStatus = connectedStatus
					pso.connected = connected
					if err != nil {
						errors <- fmt.Errorf("StartPersistentConnection: could not connect to pso: %w", err)
						continue
					}
					if !pso.connected {
						continue
					}
				}
				pso.connected = pso.checkConnection()
				if pso.connected {
					err := pso.RefreshData()
					if err != nil {
						log.Fatal(err)
						errors <- fmt.Errorf("StartPersistentConnection: could not refresh data: %w", err)
						continue
					}
				}
			case <-pso.done:
				pso.Close()
				pso.connected = false
				return
			}
		}
	}()
}

func (pso *PSO) StopPersistentConnection() {
	if pso.done != nil {
		close(pso.done)
	}
}

func (pso *PSO) Connect() (bool, string, error) {
	hwnd := w32.FindWindowW(nil, syscall.StringToUTF16Ptr(unseenWindowName))
	if hwnd == 0 {
		// unseen not found
		hwnd = w32.FindWindowW(nil, syscall.StringToUTF16Ptr(ephineaWindowName))
		if hwnd == 0 {
			return false, "Window not found", nil
		}
	}

	_, pid := w32.GetWindowThreadProcessId(hwnd)
	hndl, err := w32.OpenProcess(w32.PROCESS_ALL_ACCESS, false, uintptr(pid))
	if err != nil {
		return false, "Could not open process", fmt.Errorf("Connect: could not open process with pid %v: %w", pid, err)
	}

	if err != nil {
		return false, "Could not find base address", fmt.Errorf("Connect: could get base address: %w", err)
	}

	pso.handle = handle(hndl)

	return true, fmt.Sprintf("Connected to pid %v", pid), nil
}

func (pso *PSO) Close() {
	w32.CloseHandle(w32.HANDLE(pso.handle))
}

func (pso *PSO) CheckConnection() (bool, string) {
	return pso.connected, pso.connectedStatus
}

func (pso *PSO) checkConnection() bool {
	code, err := w32.GetExitCodeProcess(w32.HANDLE(pso.handle))
	return err == nil && code == windowsCodeStillActive
}
