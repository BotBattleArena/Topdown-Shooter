package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
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

	RoundSec     = 60
	WinKills     = 30
	CountdownSec = 3
	WebPort      = ":8090"
)

// --------------- Vector ---------------

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

// --------------- Map Objects ---------------

// MapObject is a unified geometric shape for rect, circle, poly, and bullet.
type MapObject struct {
	X      float64      `json:"x,omitempty"`
	Y      float64      `json:"y,omitempty"`
	W      float64      `json:"w,omitempty"`
	H      float64      `json:"h,omitempty"`
	R      float64      `json:"r,omitempty"`
	DX     float64      `json:"dx,omitempty"`
	DY     float64      `json:"dy,omitempty"`
	Points [][2]float64 `json:"points,omitempty"`
}

// ViewerMapObject extends MapObject with an owner ID for the web UI.
type ViewerMapObject struct {
	X      float64      `json:"x,omitempty"`
	Y      float64      `json:"y,omitempty"`
	W      float64      `json:"w,omitempty"`
	H      float64      `json:"h,omitempty"`
	R      float64      `json:"r,omitempty"`
	DX     float64      `json:"dx,omitempty"`
	DY     float64      `json:"dy,omitempty"`
	Owner  string       `json:"owner,omitempty"`
	Points [][2]float64 `json:"points,omitempty"`
}

type StaticScene struct {
	Rect   []MapObject `json:"rect,omitempty"`
	Circle []MapObject `json:"circle,omitempty"`
	Poly   []MapObject `json:"poly,omitempty"`
}

type DynamicScene struct {
	Rect    []MapObject `json:"rect,omitempty"`
	Circle  []MapObject `json:"circle,omitempty"`
	Poly    []MapObject `json:"poly,omitempty"`
	Bullets []MapObject `json:"bullets,omitempty"`
}

type SceneSetup struct {
	MapW   float64     `json:"mapw"`
	MapH   float64     `json:"maph"`
	Static StaticScene `json:"static"`
}

type SceneTick struct {
	Dynamic DynamicScene `json:"dynamic"`
}

type ViewerDynamicScene struct {
	Rect    []MapObject       `json:"rect,omitempty"`
	Circle  []MapObject       `json:"circle,omitempty"`
	Poly    []MapObject       `json:"poly,omitempty"`
	Bullets []ViewerMapObject `json:"bullets,omitempty"`
}

type ViewScene struct {
	MapW    float64            `json:"mapw"`
	MapH    float64            `json:"maph"`
	Static  StaticScene        `json:"static,omitempty"`
	Dynamic ViewerDynamicScene `json:"dynamic,omitempty"`
}

// --------------- Player ---------------

// Player is the internal game state (includes fields not sent to bots).
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

	PingTotal time.Duration
	PingCount int
	Timeouts  int

	dashing  int
	dashDir  V2
	respawnT int
}

// PlayerView is the bot-facing player data (no alive, no color).
type PlayerView struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	AimX    float64 `json:"aim_x"`
	AimY    float64 `json:"aim_y"`
	HP      int     `json:"hp"`
	Kills   int     `json:"kills"`
	Deaths  int     `json:"deaths"`
	ShootCD int     `json:"shoot_cd"`
	DashCD  int     `json:"dash_cd"`
}

func playerToView(p *Player) PlayerView {
	return PlayerView{
		ID: p.ID, X: p.X, Y: p.Y,
		AimX: p.AimX, AimY: p.AimY,
		HP: p.HP, Kills: p.Kills, Deaths: p.Deaths,
		ShootCD: p.ShootCD, DashCD: p.DashCD,
	}
}

// --------------- Bullet ---------------

type Bullet struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	DX    float64 `json:"dx"`
	DY    float64 `json:"dy"`
	Owner string  `json:"owner"`
	life  int
}

// --------------- Bot Frame (state.tick) ---------------

type BotFrame struct {
	Type    string       `json:"type"`
	Tick    int          `json:"tick"`
	Players []PlayerView `json:"players"`
	Scene   SceneTick    `json:"scene"`
}

// --------------- Kill Event ---------------

type KillEv struct {
	Killer string `json:"killer"`
	Victim string `json:"victim"`
	Tick   int    `json:"tick"`
}

// --------------- Viewer Frame (for web viewer, unchanged) ---------------

type ViewFrame struct {
	Players []*Player `json:"players"`
	Tick    int       `json:"tick"`
	MaxTick int       `json:"max_tick"`
	Kills   []KillEv  `json:"kills"`
	Over    bool      `json:"over"`
	Winner  string    `json:"winner,omitempty"`
	Scene   ViewScene `json:"scene"`
}

// --------------- Palette ---------------

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

// --------------- SSE (Viewer) ---------------

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

// --------------- Helpers ---------------

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

// --------------- Physics Helpers ---------------

func closestPointOnLineSegment(p, a, b V2) V2 {
	ab := V2{b.X - a.X, b.Y - a.Y}
	lensq := ab.X*ab.X + ab.Y*ab.Y
	if lensq < 0.0001 {
		return a
	}
	t := ((p.X-a.X)*ab.X + (p.Y-a.Y)*ab.Y) / lensq
	t = clamp(t, 0, 1)
	return V2{a.X + t*ab.X, a.Y + t*ab.Y}
}

func isPointInPoly(pt V2, poly [][2]float64) bool {
	inside := false
	j := len(poly) - 1
	for i := 0; i < len(poly); i++ {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		intersect := ((yi > pt.Y) != (yj > pt.Y)) && (pt.X < (xj-xi)*(pt.Y-yi)/(yj-yi)+xi)
		if intersect {
			inside = !inside
		}
		j = i
	}
	return inside
}

func resolveShapeArray(pos V2, r float64, objs []MapObject, shapeType string) V2 {
	for _, obj := range objs {
		if shapeType == "circle" {
			dsq := pos.DistSq(V2{obj.X, obj.Y})
			totalR := r + obj.R
			if dsq < totalR*totalR && dsq > 0.0001 {
				d := math.Sqrt(dsq)
				overlap := totalR - d
				dir := V2{pos.X - obj.X, pos.Y - obj.Y}.Mul(1 / d)
				pos = pos.Add(dir.Mul(overlap))
			}
		} else if shapeType == "rect" {
			closestX := clamp(pos.X, obj.X, obj.X+obj.W)
			closestY := clamp(pos.Y, obj.Y, obj.Y+obj.H)
			dsq := pos.DistSq(V2{closestX, closestY})
			if dsq < r*r {
				if dsq < 0.0001 {
					distLeft := pos.X - obj.X
					distRight := (obj.X + obj.W) - pos.X
					distTop := pos.Y - obj.Y
					distBottom := (obj.Y + obj.H) - pos.Y

					minDist := distLeft
					dir := V2{-1, 0}
					if distRight < minDist {
						minDist = distRight
						dir = V2{1, 0}
					}
					if distTop < minDist {
						minDist = distTop
						dir = V2{0, -1}
					}
					if distBottom < minDist {
						minDist = distBottom
						dir = V2{0, 1}
					}
					pos = pos.Add(dir.Mul(minDist + r))
				} else {
					d := math.Sqrt(dsq)
					overlap := r - d
					dir := V2{pos.X - closestX, pos.Y - closestY}.Mul(1 / d)
					pos = pos.Add(dir.Mul(overlap))
				}
			}
		} else if shapeType == "poly" && len(obj.Points) > 2 {
			inside := isPointInPoly(pos, obj.Points)
			var closestEdgePt V2
			minEdgeDistSq := math.MaxFloat64

			for i := 0; i < len(obj.Points); i++ {
				p1 := V2{obj.Points[i][0], obj.Points[i][1]}
				j := (i + 1) % len(obj.Points)
				p2 := V2{obj.Points[j][0], obj.Points[j][1]}

				cpt := closestPointOnLineSegment(pos, p1, p2)
				dsq := pos.DistSq(cpt)
				if dsq < minEdgeDistSq {
					minEdgeDistSq = dsq
					closestEdgePt = cpt
				}
			}

			if inside {
				d := math.Sqrt(minEdgeDistSq)
				dir := V2{closestEdgePt.X - pos.X, closestEdgePt.Y - pos.Y}
				if d > 0.0001 {
					dir = dir.Mul(1 / d)
					pos = pos.Add(dir.Mul(d + r))
				} else {
					pos.X += r // fallback
				}
			} else if minEdgeDistSq < r*r {
				d := math.Sqrt(minEdgeDistSq)
				if d > 0.0001 {
					overlap := r - d
					dir := V2{pos.X - closestEdgePt.X, pos.Y - closestEdgePt.Y}.Mul(1 / d)
					pos = pos.Add(dir.Mul(overlap))
				}
			}
		}
	}
	return pos
}

func resolveCollisions(pos V2, r float64, static StaticScene, dyn DynamicScene) V2 {
	pos = resolveShapeArray(pos, r, static.Rect, "rect")
	pos = resolveShapeArray(pos, r, static.Circle, "circle")
	pos = resolveShapeArray(pos, r, static.Poly, "poly")

	pos = resolveShapeArray(pos, r, dyn.Rect, "rect")
	pos = resolveShapeArray(pos, r, dyn.Circle, "circle")
	pos = resolveShapeArray(pos, r, dyn.Poly, "poly")
	return pos
}

// --------------- Static Map Definition ---------------

func buildStaticMap() StaticScene {
	return StaticScene{
		Rect: []MapObject{
			{X: 0, Y: 0, W: MapW, H: 50},
			{X: 0, Y: 0, W: 50, H: MapH},
			{X: MapW - 50, Y: 0, W: 50, H: MapH},
			{X: 0, Y: MapH - 50, W: MapW, H: 50},
			{X: 900, Y: 900, W: 200, H: 200},
		},
		Circle: []MapObject{
			{X: 500, Y: 500, R: 80},
			{X: 1500, Y: 1500, R: 80},
		},
		Poly: []MapObject{
			{Points: [][2]float64{{300, 1400}, {400, 1300}, {500, 1400}}},
			{Points: [][2]float64{{1500, 600}, {1600, 500}, {1700, 600}}},
		},
	}
}

// buildDynamicMap returns dynamic map objects that change each tick.
func buildDynamicMap(tick int) DynamicScene {
	// A rect that oscillates horizontally
	baseX := 650.0
	offsetX := math.Sin(float64(tick)/60.0) * 200.0
	return DynamicScene{
		Rect: []MapObject{
			{X: baseX + offsetX, Y: 1000, W: 120, H: 40},
		},
	}
}

// --------------- Main ---------------

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

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: topdownshooter [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	inputDir := flag.String("input-dir", "./bots/inputs", "(string) path to bots input directory")
	flag.Parse()

	a, err := arena.New(arena.Config{
		InputDir:      *inputDir,
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
		log.Fatalf("No bots in %s", *inputDir)
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

	// ========== Phase 0: Setup ==========
	staticMap := buildStaticMap()

	setup := map[string]interface{}{
		"type":             "setup",
		"tick_duration_ms": 1000.0 / TPS,
		"timeout_ms":       AxesTimeout.Seconds() * 1000,
		"max_tick":         maxTick,
		"countdown_sec":    CountdownSec,
		"scene": SceneSetup{
			MapW:   MapW,
			MapH:   MapH,
			Static: staticMap,
		},
		"rules": map[string]interface{}{
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
	fmt.Println("  Sending setup frame...")
	a.SendState(setupData)

	// ========== Countdown ==========
	for i := CountdownSec; i > 0; i-- {
		fmt.Printf("  Starting in %d...\n", i)
		time.Sleep(time.Second)
	}
	fmt.Println("  GO!")

	// ========== Phase 1: Game Loop ==========
	ticker := time.NewTicker(TickDur)
	defer ticker.Stop()

	for range ticker.C {
		if gameOver {
			break
		}
		tick++

		// Build dynamic scene including map objects and bullets
		dynMap := buildDynamicMap(tick)
		for _, b := range bullets {
			dynMap.Bullets = append(dynMap.Bullets, MapObject{
				X: b.X, Y: b.Y, R: BRadius, DX: b.DX, DY: b.DY,
			})
		}

		// Build bot-facing player list (no alive, no color)
		pvs := make([]PlayerView, 0, len(players))
		for _, p := range players {
			pvs = append(pvs, playerToView(p))
		}

		bf := BotFrame{
			Type:    "tick",
			Tick:    tick,
			Players: pvs,
			Scene:   SceneTick{Dynamic: dynMap},
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

			axes := make(map[string]float32)
			if res, ok := resp[id]; ok {
				if res.TimedOut {
					p.Timeouts++
				} else {
					p.PingTotal += res.Duration
					p.PingCount++
				}
				axes = res.Axes
			}

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

			newPos := resolveCollisions(V2{p.X, p.Y}, PRadius, staticMap, dynMap)
			p.X, p.Y = newPos.X, newPos.Y

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

			bPos := V2{b.X, b.Y}
			resolved := resolveCollisions(bPos, BRadius, staticMap, dynMap)
			if resolved.DistSq(bPos) > 0.0001 {
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

		// Emit viewer frame (every 3 ticks)
		if tick%3 == 0 {
			pList := make([]*Player, 0, len(players))
			for _, p := range players {
				pList = append(pList, p)
			}

			// Rebuild dynamic map but with bullets containing owners for the viewer
			baseDyn := buildDynamicMap(tick)
			viewDyn := ViewerDynamicScene{
				Rect:   baseDyn.Rect,
				Circle: baseDyn.Circle,
				Poly:   baseDyn.Poly,
			}
			for _, b := range bullets {
				viewDyn.Bullets = append(viewDyn.Bullets, ViewerMapObject{
					X: b.X, Y: b.Y, R: BRadius, DX: b.DX, DY: b.DY, Owner: b.Owner,
				})
			}

			vf := ViewFrame{
				Players: pList,
				Tick:    tick, MaxTick: maxTick,
				Kills: kills, Over: gameOver, Winner: winner,
				Scene: ViewScene{MapW: MapW, MapH: MapH, Static: staticMap, Dynamic: viewDyn},
			}
			data, _ := json.Marshal(vf)
			emit(data)
		}
	}

	// Final viewer frame
	pList := make([]*Player, 0, len(players))
	for _, p := range players {
		pList = append(pList, p)
	}
	baseDynFinal := buildDynamicMap(tick)
	viewDynFinal := ViewerDynamicScene{
		Rect:   baseDynFinal.Rect,
		Circle: baseDynFinal.Circle,
		Poly:   baseDynFinal.Poly,
	}
	for _, b := range bullets {
		viewDynFinal.Bullets = append(viewDynFinal.Bullets, ViewerMapObject{
			X: b.X, Y: b.Y, R: BRadius, DX: b.DX, DY: b.DY, Owner: b.Owner,
		})
	}
	final := ViewFrame{
		Players: pList,
		Tick:    tick, MaxTick: maxTick,
		Kills: kills, Over: true, Winner: winner,
		Scene: ViewScene{MapW: MapW, MapH: MapH, Static: staticMap, Dynamic: viewDynFinal},
	}
	fd, _ := json.Marshal(final)
	emit(fd)

	fmt.Println("\n=== Round Over ===")
	fmt.Printf("Winner: %s\n", winner)
	type lbEntry struct {
		id       string
		k        int
		avgPing  time.Duration
		timeouts int
	}
	lb := make([]lbEntry, 0, len(players))
	for _, p := range players {
		avg := time.Duration(0)
		if p.PingCount > 0 {
			avg = p.PingTotal / time.Duration(p.PingCount)
		}
		lb = append(lb, lbEntry{p.ID, p.Kills, avg, p.Timeouts})
	}
	sort.Slice(lb, func(i, j int) bool { return lb[i].k > lb[j].k })
	for i, e := range lb {
		fmt.Printf("  #%d %s — %d kills | avg ping: %dµs | timeouts: %d\n",
			i+1, e.id, e.k, e.avgPing.Microseconds(), e.timeouts)
	}
	time.Sleep(5 * time.Second)
}
