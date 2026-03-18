package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"flag"

	"github.com/BotBattleArena/ArenaFramework/pkg/arena"
)

const (
	MapW float64 = 2000
	MapH float64 = 2000

	TPS         = 60
	TickDur     = time.Second / TPS
	AxesTimeout = 12 * time.Millisecond

	MoveSpeed  = 3.5
	DashSpd    = 14.0
	DashLen    = 8
	DashCD     = 180
	BulletSpd  = 10.0
	BulletLife = 90
	ShootCD    = 25
	Dmg        = 34
	MaxHP      = 100
	PRadius    = 15.0
	BRadius    = 4.0
	RespawnT   = 120

	RoundSec = 60
	WinKills = 30
	WebPort  = ":8090"
)

type V2 struct{ X, Y float64 }

func (a V2) Add(b V2) V2      { return V2{a.X + b.X, a.Y + b.Y} }
func (a V2) Mul(s float64) V2 { return V2{a.X * s, a.Y * s} }
func (a V2) Len() float64     { return math.Sqrt(a.X*a.X + a.Y*a.Y) }
func (a V2) Norm() V2 {
	l := a.Len()
	if l < 1e-6 {
		return V2{}
	}
	return V2{a.X / l, a.Y / l}
}
func (a V2) DistSq(b V2) float64 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

type Player struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	AimX    float64 `json:"aim_x"`
	AimY    float64 `json:"aim_y"`
	HP      int     `json:"hp"`
	Kills   int     `json:"kills"`
	Deaths  int     `json:"deaths"`
	Alive   bool    `json:"alive"`
	ShootCD int     `json:"shoot_cd"`
	DashCD  int     `json:"dash_cd"`
	Color   string  `json:"color"`

	dashing  int
	dashDir  V2
	respawnT int
}

type Bullet struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	DX    float64 `json:"dx"`
	DY    float64 `json:"dy"`
	Owner string  `json:"owner"`
	life  int
}

type KillEv struct {
	Killer string `json:"killer"`
	Victim string `json:"victim"`
	Tick   int    `json:"tick"`
}

type BotFrame struct {
	Players map[string]*Player `json:"players"`
	Bullets []BulletView       `json:"bullets"`
	MapW    float64            `json:"map_w"`
	MapH    float64            `json:"map_h"`
	Tick    int                `json:"tick"`
	Left    int                `json:"time_left"`
}

type BulletView struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	DX    float64 `json:"dx"`
	DY    float64 `json:"dy"`
	Owner string  `json:"owner"`
}

type ViewFrame struct {
	Players []*Player    `json:"players"`
	Bullets []BulletView `json:"bullets"`
	MapW    float64      `json:"map_w"`
	MapH    float64      `json:"map_h"`
	Tick    int          `json:"tick"`
	MaxTick int          `json:"max_tick"`
	Kills   []KillEv     `json:"kills"`
	Over    bool         `json:"over"`
	Winner  string       `json:"winner,omitempty"`
}

var palette = []string{
	"#e74c3c", "#2ecc71", "#3498db", "#f1c40f", "#e67e22", "#9b59b6",
	"#1abc9c", "#e84393", "#00cec9", "#fd79a8", "#6c5ce7", "#ffeaa7",
	"#d63031", "#00b894", "#0984e3", "#fdcb6e", "#e17055", "#a29bfe",
	"#55efc4", "#fab1a0", "#74b9ff", "#ff7675", "#00b4d8", "#81ecec",
	"#ff9ff3", "#54a0ff", "#5f27cd", "#01a3a4", "#ee5a24", "#009432",
	"#0652DD", "#9980FA", "#FFC312", "#ED4C67", "#12CBC4", "#FDA7DF",
	"#B53471", "#A3CB38", "#1289A7", "#D980FA", "#F79F1F", "#EA2027",
	"#006266", "#1B1464", "#5758BB", "#6F1E51", "#833471", "#0a3d62",
	"#3c6382", "#60a3bc", "#6a89cc", "#82ccdd", "#b8e994", "#f8c291",
	"#e55039", "#4a69bd", "#e58e26", "#b71540", "#079992", "#78e08f",
	"#e1d89f", "#f6b93b", "#fa983a", "#eb2f06", "#1e3799", "#b33939",
	"#cc8e35", "#cd6133", "#7158e2", "#474787", "#227093", "#218c74",
	"#2c2c54", "#aaa69d", "#d1ccc0", "#ff6348", "#ffa502", "#3742fa",
	"#2ed573", "#1e90ff", "#ff4757", "#70a1ff", "#7bed9f", "#ff6b81",
	"#eccc68", "#a4b0be", "#57606f", "#747d8c", "#2f3542", "#dfe4ea",
	"#ced6e0", "#f7d794", "#f3a683", "#786fa6", "#cf6a87", "#e77f67",
	"#63cdda", "#ea8685", "#596275", "#303952", "#574b90", "#f78fb3",
}

type Config struct {
	InputDir string `json:"input_dir"`
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&cfg)
	return cfg, err
}

var (
	sseCh    = make(map[chan []byte]struct{})
	sseMu    sync.Mutex
	lastSnap []byte
)

func emit(data []byte) {
	sseMu.Lock()
	lastSnap = data
	for c := range sseCh {
		select {
		case c <- data:
		default:
		}
	}
	sseMu.Unlock()
}

func eventsEndpoint(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 4)
	sseMu.Lock()
	sseCh[ch] = struct{}{}
	if lastSnap != nil {
		ch <- lastSnap
	}
	sseMu.Unlock()
	defer func() {
		sseMu.Lock()
		delete(sseCh, ch)
		sseMu.Unlock()
	}()

	for {
		select {
		case d := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", d)
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func rndSpawn() V2 {
	m := 80.0
	return V2{m + rand.Float64()*(MapW-2*m), m + rand.Float64()*(MapH-2*m)}
}

func main() {
	maxTick := RoundSec * TPS

	fmt.Println("=== Top-Down Shooter ===")
	fmt.Printf("Map: %.0fx%.0f | %ds round | %d kills to win\n", MapW, MapH, RoundSec, WinKills)
	fmt.Printf("Viewer: http://localhost%s\n\n", WebPort)

	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html;charset=utf-8")
			fmt.Fprint(w, viewerHTML)
		})
		http.HandleFunc("/events", eventsEndpoint)
		log.Fatal(http.ListenAndServe(WebPort, nil))
	}()

	inputDirFlag := flag.String("input-dir", "", "path to bots input directory")
	flag.Parse()

	var cfg Config
	var err error

	if *inputDirFlag != "" {
		cfg.InputDir = *inputDirFlag
	} else {
		cfg, err = loadConfig("config.json")
		if err != nil {
			log.Printf("Warning: configuration not found or invalid, using defaults: %v", err)
			cfg = Config{InputDir: "./bots/inputs"}
		}
	}

	a, err := arena.New(arena.Config{
		InputDir:      cfg.InputDir,
		ActionTimeout: AxesTimeout,
		Axes: []arena.Axis{
			{Name: "move_x", Value: 0},
			{Name: "move_y", Value: 0},
			{Name: "aim_x", Value: 0},
			{Name: "aim_y", Value: 1},
			{Name: "shoot", Value: 0},
			{Name: "dash", Value: 0},
		},
	})
	if err != nil {
		log.Fatalf("Arena: %v", err)
	}

	a.OnConnect(func(p arena.Player) { fmt.Printf("  + %s\n", p.ID) })

	if err := a.Start(); err != nil {
		log.Fatalf("Start: %v", err)
	}
	defer a.Stop()

	bots := a.Players()
	if len(bots) == 0 {
		log.Fatalf("No bots in %s", cfg.InputDir)
	}
	fmt.Printf("  %d bots loaded\n\n", len(bots))

	players := make(map[string]*Player, len(bots))
	for i, b := range bots {
		sp := rndSpawn()
		players[b.ID] = &Player{
			ID: b.ID, X: sp.X, Y: sp.Y,
			AimX: 0, AimY: 1, HP: MaxHP,
			Alive: true, Color: palette[i%len(palette)],
		}
	}

	bullets := make([]*Bullet, 0, 256)
	kills := make([]KillEv, 0, 64)
	gameOver := false
	winner := ""
	tick := 0

	// Phase 0: Initialization
	setup := map[string]interface{}{
		"type":             "setup",
		"tps":              TPS,
		"tick_duration_ms": 1000.0 / TPS,
		"timeout_ms":       AxesTimeout.Seconds() * 1000,
		"rules": map[string]interface{}{
			"map_w":         MapW,
			"map_h":         MapH,
			"move_speed":    MoveSpeed,
			"bullet_speed":  BulletSpd,
			"damage":        Dmg,
			"win_kills":     WinKills,
			"respawn_ticks": RespawnT,
			"shoot_cd":      ShootCD,
			"dash_cd":       DashCD,
		},
	}
	setupData, _ := json.Marshal(setup)
	fmt.Println("  Sending initialization frame...")
	a.RequestAxes(setupData, 100*time.Millisecond) // Give bots more time to initialize

	ticker := time.NewTicker(TickDur)
	defer ticker.Stop()

	for range ticker.C {
		if gameOver {
			break
		}
		tick++

		bvs := make([]BulletView, len(bullets))
		for i, b := range bullets {
			bvs[i] = BulletView{b.X, b.Y, b.DX, b.DY, b.Owner}
		}
		bf := BotFrame{
			Players: players, Bullets: bvs,
			MapW: MapW, MapH: MapH,
			Tick: tick, Left: (maxTick - tick) / TPS,
		}
		state, _ := json.Marshal(bf)
		resp := a.RequestAxes(state, AxesTimeout)

		for id, p := range players {
			if p.respawnT > 0 {
				p.respawnT--
				if p.respawnT == 0 {
					p.Alive = true
					p.HP = MaxHP
					sp := rndSpawn()
					p.X, p.Y = sp.X, sp.Y
				}
				continue
			}
			if !p.Alive {
				continue
			}

			axes := resp[id]
			mx := clamp(float64(axes["move_x"]), -1, 1)
			my := clamp(float64(axes["move_y"]), -1, 1)
			ax := clamp(float64(axes["aim_x"]), -1, 1)
			ay := clamp(float64(axes["aim_y"]), -1, 1)
			wantShoot := axes["shoot"] > 0
			wantDash := axes["dash"] > 0

			aim := V2{ax, ay}.Norm()
			if aim.Len() > 0.01 {
				p.AimX, p.AimY = aim.X, aim.Y
			}

			if p.dashing > 0 {
				pos := V2{p.X, p.Y}.Add(p.dashDir.Mul(DashSpd))
				p.X, p.Y = pos.X, pos.Y
				p.dashing--
			} else {
				mv := V2{mx, my}
				if mv.Len() > 1.0 {
					mv = mv.Norm()
				}
				pos := V2{p.X, p.Y}.Add(mv.Mul(MoveSpeed))
				p.X, p.Y = pos.X, pos.Y
			}

			p.X = clamp(p.X, PRadius, MapW-PRadius)
			p.Y = clamp(p.Y, PRadius, MapH-PRadius)

			if p.ShootCD > 0 {
				p.ShootCD--
			}
			if p.DashCD > 0 {
				p.DashCD--
			}

			if wantShoot && p.ShootCD == 0 {
				ad := V2{p.AimX, p.AimY}
				if ad.Len() > 0.01 {
					p.ShootCD = ShootCD
					off := ad.Mul(PRadius + BRadius + 2)
					bullets = append(bullets, &Bullet{
						X: p.X + off.X, Y: p.Y + off.Y,
						DX: ad.X, DY: ad.Y,
						Owner: id, life: BulletLife,
					})
				}
			}

			if wantDash && p.DashCD == 0 && p.dashing == 0 {
				p.DashCD = DashCD
				p.dashing = DashLen
				p.dashDir = V2{p.AimX, p.AimY}
			}
		}

		hitR := (PRadius + BRadius) * (PRadius + BRadius)
		alive := bullets[:0]
		for _, b := range bullets {
			b.X += b.DX * BulletSpd
			b.Y += b.DY * BulletSpd
			b.life--

			if b.X < 0 || b.X > MapW || b.Y < 0 || b.Y > MapH || b.life <= 0 {
				continue
			}

			hit := false
			for pid, p := range players {
				if pid == b.Owner || !p.Alive {
					continue
				}
				pp := V2{p.X, p.Y}
				bp := V2{b.X, b.Y}
				if pp.DistSq(bp) < hitR {
					p.HP -= Dmg
					if p.HP <= 0 {
						p.HP = 0
						p.Alive = false
						p.Deaths++
						p.respawnT = RespawnT
						if att, ok := players[b.Owner]; ok {
							att.Kills++
							kills = append(kills, KillEv{b.Owner, pid, tick})
							if len(kills) > 10 {
								kills = kills[len(kills)-10:]
							}
							if att.Kills >= WinKills {
								gameOver = true
								winner = b.Owner
							}
						}
					}
					hit = true
					break
				}
			}
			if !hit {
				alive = append(alive, b)
			}
		}
		bullets = alive

		if tick >= maxTick && !gameOver {
			gameOver = true
			bk := -1
			bd := 99999
			for _, p := range players {
				if p.Kills > bk || (p.Kills == bk && p.Deaths < bd) {
					bk = p.Kills
					bd = p.Deaths
					winner = p.ID
				}
			}
		}

		if tick%3 == 0 {
			pList := make([]*Player, 0, len(players))
			for _, p := range players {
				pList = append(pList, p)
			}
			bvs2 := make([]BulletView, len(bullets))
			for i, b := range bullets {
				bvs2[i] = BulletView{b.X, b.Y, b.DX, b.DY, b.Owner}
			}
			vf := ViewFrame{
				Players: pList, Bullets: bvs2,
				MapW: MapW, MapH: MapH,
				Tick: tick, MaxTick: maxTick,
				Kills: kills, Over: gameOver, Winner: winner,
			}
			data, _ := json.Marshal(vf)
			emit(data)
		}
	}

	pList := make([]*Player, 0, len(players))
	for _, p := range players {
		pList = append(pList, p)
	}
	bvsFinal := make([]BulletView, len(bullets))
	for i, b := range bullets {
		bvsFinal[i] = BulletView{b.X, b.Y, b.DX, b.DY, b.Owner}
	}
	final := ViewFrame{
		Players: pList, Bullets: bvsFinal,
		MapW: MapW, MapH: MapH,
		Tick: tick, MaxTick: maxTick,
		Kills: kills, Over: true, Winner: winner,
	}
	fd, _ := json.Marshal(final)
	emit(fd)

	fmt.Println("\n=== Round Over ===")
	fmt.Printf("Winner: %s\n", winner)
	lb := make([]struct {
		id string
		k  int
	}, 0, len(players))
	for _, p := range players {
		lb = append(lb, struct {
			id string
			k  int
		}{p.ID, p.Kills})
	}
	sort.Slice(lb, func(i, j int) bool { return lb[i].k > lb[j].k })
	for i, e := range lb {
		fmt.Printf("  #%d %s — %d kills\n", i+1, e.id, e.k)
	}
	time.Sleep(5 * time.Second)
}
