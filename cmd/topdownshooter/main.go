package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"flag"

	"github.com/BotBattleArena/ArenaFramework/pkg/arena"
)

var (
	MapW     float64 = 2000
	MapH     float64 = 2000
	RoundSec int     = 60
	WinKills int     = 30
)

const (
	TPS         = 60
	TickDur     = time.Second / TPS
	AxesTimeout = 12 * time.Millisecond

	MoveSpeed  = 3.5
	DashSpd    = 14.0
	DashLen    = 8
	DashCD     = 180
	BulletSpd  = 10.0
	BulletLife = 600
	ShootCD    = 25
	Dmg        = 34
	MaxHP      = 100
	PRadius    = 15.0
	BRadius    = 4.0
	RespawnT   = 120

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
		X: p.X, Y: p.Y,
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
	Type    string                `json:"type"`
	Tick    int                   `json:"tick"`
	Players map[string]PlayerView `json:"players"`
	Scene   SceneTick             `json:"scene"`
}

// --------------- Kill Event ---------------

type KillEv struct {
	Killer string `json:"killer"`
	Victim string `json:"victim"`
	Tick   int    `json:"tick"`
}

// --------------- Viewer Frame (for web viewer, unchanged) ---------------

type ViewFrame struct {
	Players map[string]*Player `json:"players"`
	Tick    int                `json:"tick"`
	MaxTick int                `json:"max_tick"`
	Kills   []KillEv           `json:"kills"`
	Over    bool               `json:"over"`
	Winner  string             `json:"winner,omitempty"`
	Scene   ViewScene          `json:"scene"`
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

// --------------- Generated Map Definition ---------------

type GenDoor struct {
	X, Y, W, H float64
	Phase      int
	Vertical   bool
}

var globalDoors []GenDoor

func generateMap(w, h, credits float64) StaticScene {
	globalDoors = nil
	static := StaticScene{}

	// Outer walls (50px thick border)
	wallT := 50.0
	static.Rect = append(static.Rect, MapObject{X: 0, Y: 0, W: w, H: wallT})         // top
	static.Rect = append(static.Rect, MapObject{X: 0, Y: 0, W: wallT, H: h})         // left
	static.Rect = append(static.Rect, MapObject{X: w - wallT, Y: 0, W: wallT, H: h}) // right
	static.Rect = append(static.Rect, MapObject{X: 0, Y: h - wallT, W: w, H: wallT}) // bottom

	// Grid: cell size relative to map (smaller side / 15), clamped to [120, 350]
	minSide := w
	if h < minSide {
		minSide = h
	}
	cellSize := clamp(minSide/15, 120, 350)
	margin := 150.0
	playW := w - 2*margin
	playH := h - 2*margin
	cols := int(playW / cellSize)
	rows := int(playH / cellSize)
	if cols < 3 {
		cols = 3
	}
	if rows < 3 {
		rows = 3
	}

	// Limit the number of structures based on grid density instead of just credits
	maxObjects := (cols * rows) / 5
	placedCount := 0

	usedGrid := make([][]bool, cols)
	for i := range usedGrid {
		usedGrid[i] = make([]bool, rows)
	}

	// gridOrigin: top-left corner of grid cell gx,gy with random jitter
	gridOrigin := func(gx, gy int) (float64, float64) {
		jitter := cellSize * 0.15
		ox := margin + float64(gx)*cellSize + (rand.Float64()*2-1)*jitter
		oy := margin + float64(gy)*cellSize + (rand.Float64()*2-1)*jitter
		return ox, oy
	}
	// gridCenter: center of grid cell gx,gy with random jitter
	gridCenter := func(gx, gy int) (float64, float64) {
		ox, oy := gridOrigin(gx, gy)
		return ox + cellSize/2, oy + cellSize/2
	}

	// --- Helper: horizontal wall with centered doorway ---
	addHWall := func(x, y, length, thick, doorW float64, addDoor bool) {
		minStub := thick // each side must be at least this wide
		if doorW > length-2*minStub {
			doorW = length - 2*minStub
		}
		if doorW < minStub { // wall too short for any door → solid
			static.Rect = append(static.Rect, MapObject{X: x, Y: y, W: length, H: thick})
			return
		}
		half := (length - doorW) / 2
		static.Rect = append(static.Rect, MapObject{X: x, Y: y, W: half, H: thick})
		static.Rect = append(static.Rect, MapObject{X: x + half + doorW, Y: y, W: half, H: thick})
		if addDoor {
			globalDoors = append(globalDoors, GenDoor{
				X: x + half, Y: y, W: doorW, H: thick,
				Phase: rand.Intn(300), Vertical: false,
			})
		}
	}

	// --- Helper: vertical wall with centered doorway ---
	addVWall := func(x, y, length, thick, doorH float64, addDoor bool) {
		minStub := thick
		if doorH > length-2*minStub {
			doorH = length - 2*minStub
		}
		if doorH < minStub { // wall too short for any door → solid
			static.Rect = append(static.Rect, MapObject{X: x, Y: y, W: thick, H: length})
			return
		}
		half := (length - doorH) / 2
		static.Rect = append(static.Rect, MapObject{X: x, Y: y, W: thick, H: half})
		static.Rect = append(static.Rect, MapObject{X: x, Y: y + half + doorH, W: thick, H: half})
		if addDoor {
			globalDoors = append(globalDoors, GenDoor{
				X: x, Y: y + half, W: thick, H: doorH,
				Phase: rand.Intn(300), Vertical: true,
			})
		}
	}

	// --- Helper: solid horizontal wall ---
	addHSolid := func(x, y, length, thick float64) {
		static.Rect = append(static.Rect, MapObject{X: x, Y: y, W: length, H: thick})
	}

	// --- Helper: solid vertical wall ---
	addVSolid := func(x, y, length, thick float64) {
		static.Rect = append(static.Rect, MapObject{X: x, Y: y, W: thick, H: length})
	}

	// Mark grid block as used + a 1-cell buffer around it to ensure distance
	markUsed := func(gx, gy, bw, bh int) {
		for dx := -1; dx <= bw; dx++ {
			for dy := -1; dy <= bh; dy++ {
				nx, ny := gx+dx, gy+dy
				if nx >= 0 && nx < cols && ny >= 0 && ny < rows {
					usedGrid[nx][ny] = true
				}
			}
		}
	}

	budget := credits
	thick := 25.0
	doorW := 200.0 // for interior dynamic doors

	// addBox: creates a rectangular room at (bx,by) size (bw,bh).
	// Walls overlap at corners (each extends full dimension).
	// openSide: 0=top open, 1=bottom open, 2=left open, 3=right open, -1=all closed.
	addBox := func(bx, by, bw, bh, wallThick float64, openSide int) {
		if openSide != 0 { // top
			addHSolid(bx, by, bw, wallThick)
		}
		if openSide != 1 { // bottom
			addHSolid(bx, by+bh-wallThick, bw, wallThick)
		}
		if openSide != 2 { // left
			addVSolid(bx, by, bh, wallThick)
		}
		if openSide != 3 { // right
			addVSolid(bx+bw-wallThick, by, bh, wallThick)
		}
	}

	// ═══════════ Tier 0: Large complex (3×3 grid, cost 100) ═══════════
	for credits > budget*0.6+150 && placedCount < maxObjects {
		gx, gy, ok := getFreeGridBlock(cols, rows, 3, 3, usedGrid)
		if !ok {
			break
		}
		ox, oy := gridOrigin(gx, gy)
		cw := cellSize * 2.7 // slightly smaller than 3*3 to allow jitter
		ch := cellSize * 2.7

		variant := rand.Intn(3)
		switch variant {
		case 0: // Cross-divided 4-room complex, 1 open entrance side
			openSide := rand.Intn(4)
			addBox(ox, oy, cw, ch, thick, openSide)
			// Interior cross walls with dynamic doors
			midX := ox + cw/2 - thick/2
			midY := oy + ch/2 - thick/2
			innerH := ch - 2*thick
			innerW := cw - 2*thick
			addVWall(midX, oy+thick, innerH/2, thick, doorW, true)
			addVWall(midX, oy+thick+innerH/2, innerH/2, thick, doorW, true)
			addHWall(ox+thick, midY, innerW/2-thick/2, thick, doorW, true)
			addHWall(midX+thick, midY, innerW/2-thick/2, thick, doorW, true)

		case 1: // L-shaped compound: two rooms joined at corner
			roomA_W := cw * 0.6
			roomA_H := ch * 0.55
			roomB_W := cw * 0.55
			roomB_H := ch * 0.6
			// Room A (top-left area)
			addHWall(ox, oy, roomA_W, thick, doorW, false)               // top
			addVSolid(ox, oy, roomA_H, thick)                            // left
			addHSolid(ox, oy+roomA_H-thick, roomA_W, thick)              // bottom of A
			addVWall(ox+roomA_W-thick, oy, roomA_H, thick, doorW, false) // right of A (entrance to corridor)
			// Room B (bottom-right area)
			addVWall(ox+cw-roomB_W, oy+ch-roomB_H, roomB_H, thick, doorW, false) // left of B (entrance)
			addHSolid(ox+cw-roomB_W, oy+ch-roomB_H, roomB_W, thick)              // top of B
			addVSolid(ox+cw-thick, oy+ch-roomB_H, roomB_H, thick)                // right
			addHWall(ox+cw-roomB_W, oy+ch-thick, roomB_W, thick, doorW, false)   // bottom
			// Connecting corridor walls between A and B
			corrY := oy + roomA_H - thick
			corrH := ch - roomA_H - roomB_H + 2*thick
			if corrH > thick*2 {
				addHSolid(ox+roomA_W-thick, corrY, cw-roomA_W-roomB_W+2*thick, thick)
			}

		case 2: // Corridor layout: two rooms with corridor between
			roomH := ch*0.35 + rand.Float64()*ch*0.1
			corrH := ch - 2*roomH
			corrW := cw * 0.35
			corrOx := ox + (cw-corrW)/2
			// Top room
			addHWall(ox, oy, cw, thick, doorW, false)       // top entrance
			addVSolid(ox, oy, roomH, thick)                 // left
			addVSolid(ox+cw-thick, oy, roomH, thick)        // right
			addHSolid(ox, oy+roomH-thick, corrOx-ox, thick) // bottom-left partition
			addHSolid(corrOx+corrW, oy+roomH-thick, ox+cw-corrOx-corrW, thick)
			// Corridor
			addVSolid(corrOx, oy+roomH-thick, corrH+thick, thick)             // left wall
			addVSolid(corrOx+corrW-thick, oy+roomH-thick, corrH+thick, thick) // right wall
			// Bottom room
			addHSolid(ox, oy+ch-roomH, corrOx-ox, thick) // top-left partition
			addHSolid(corrOx+corrW, oy+ch-roomH, ox+cw-corrOx-corrW, thick)
			addVSolid(ox, oy+ch-roomH, roomH, thick)           // left
			addVSolid(ox+cw-thick, oy+ch-roomH, roomH, thick)  // right
			addHWall(ox, oy+ch-thick, cw, thick, doorW, false) // bottom entrance
			// Dynamic doors in corridor
			globalDoors = append(globalDoors, GenDoor{
				X: corrOx, Y: oy + roomH - thick + corrH/2, W: corrW, H: thick,
				Phase: rand.Intn(300), Vertical: false,
			})
		}

		placedCount++
		markUsed(gx, gy, 3, 3)
		credits -= 75
	}

	// ═══════════ Tier 1: Medium building (2×2 grid, cost 50) ═══════════
	for credits > budget*0.5+100 && placedCount < maxObjects {
		gx, gy, ok := getFreeGridBlock(cols, rows, 2, 2, usedGrid)
		if !ok {
			break
		}
		ox, oy := gridOrigin(gx, gy)
		bw := cellSize * 1.7 // slightly smaller than 2*2 to allow jitter
		bh := cellSize * 1.7

		variant := rand.Intn(3)
		switch variant {
		case 0: // Rectangular with interior wall, 1 open entrance side
			openSide := rand.Intn(4)
			addBox(ox, oy, bw, bh, thick, openSide)
			// Interior wall with dynamic door
			if rand.Float64() < 0.7 {
				if rand.Intn(2) == 0 {
					midX := ox + bw/2 - thick/2
					addVWall(midX, oy+thick, bh-2*thick, thick, doorW, true)
				} else {
					midY := oy + bh/2 - thick/2
					addHWall(ox+thick, midY, bw-2*thick, thick, doorW, true)
				}
			}

		case 1: // L-shaped building: two open wings joined at corner
			wingW := bw * (0.45 + rand.Float64()*0.15)
			wingH := bh * (0.45 + rand.Float64()*0.15)
			// Top wing (horizontal, spans full width, height = wingH)
			addHWall(ox, oy, bw, thick, doorW, false)   // top with entrance
			addVSolid(ox, oy, wingH, thick)             // left
			addHSolid(ox, oy+wingH-thick, wingW, thick) // bottom of top wing (partial, creates L corner)
			addVSolid(ox+bw-thick, oy, wingH, thick)    // right of top wing
			// Right wing (vertical, extends down from top wing)
			addVSolid(ox+bw-thick, oy+wingH, bh-wingH, thick)                // right continues down
			addHWall(ox+wingW, oy+bh-thick, bw-wingW, thick, doorW, false)   // bottom with entrance
			addVSolid(ox+wingW-thick, oy+wingH-thick, bh-wingH+thick, thick) // inner left of lower wing

		case 2: // U-shaped open courtyard with optional closing gate
			openSide := rand.Intn(4)
			switch openSide {
			case 0: // open top
				addHWall(ox, oy+bh-thick, bw, thick, doorW, false) // bottom entrance
				addVSolid(ox, oy, bh, thick)
				addVSolid(ox+bw-thick, oy, bh, thick)
				// partial top walls (arms of U)
				addHSolid(ox, oy, bw*0.3, thick)
				addHSolid(ox+bw*0.7, oy, bw*0.3, thick)
			case 1: // open bottom
				addHWall(ox, oy, bw, thick, doorW, false) // top entrance
				addVSolid(ox, oy, bh, thick)
				addVSolid(ox+bw-thick, oy, bh, thick)
				addHSolid(ox, oy+bh-thick, bw*0.3, thick)
				addHSolid(ox+bw*0.7, oy+bh-thick, bw*0.3, thick)
			case 2: // open left
				addHSolid(ox, oy, bw, thick)
				addHSolid(ox, oy+bh-thick, bw, thick)
				addVWall(ox+bw-thick, oy, bh, thick, doorW, false) // right entrance
				addVSolid(ox, oy, bh*0.3, thick)
				addVSolid(ox, oy+bh*0.7, bh*0.3, thick)
			case 3: // open right
				addHSolid(ox, oy, bw, thick)
				addHSolid(ox, oy+bh-thick, bw, thick)
				addVWall(ox, oy, bh, thick, doorW, false) // left entrance
				addVSolid(ox+bw-thick, oy, bh*0.3, thick)
				addVSolid(ox+bw-thick, oy+bh*0.7, bh*0.3, thick)
			}
			// Add a circle pillar in the courtyard center
			static.Circle = append(static.Circle, MapObject{
				X: ox + bw/2, Y: oy + bh/2, R: 20 + rand.Float64()*25,
			})
		}

		placedCount++
		markUsed(gx, gy, 2, 2)
		credits -= 50
	}

	// ═══════════ Tier 2: Small structures (1×1 grid, cost 20) ═══════════
	// Various asymmetric shapes: L, U, T, and rectangular buildings
	for credits > budget*0.25+50 && placedCount < maxObjects {
		gx, gy, ok := getFreeGridCell(cols, rows, usedGrid)
		if !ok {
			break
		}
		ox, oy := gridOrigin(gx, gy)
		sThick := 20.0
		// Space within 1x1 cell, slightly reduced for jitter padding
		space := cellSize * 0.8
		ox += (cellSize - space) / 2
		oy += (cellSize - space) / 2

		variant := rand.Intn(4)
		switch variant {
		case 0: // L-wall (two walls at right angle, open structure)
			armLen := space * (0.6 + rand.Float64()*0.2)
			cx := ox + space/2
			cy := oy + space/2
			corner := rand.Intn(4)
			switch corner {
			case 0: // top-left corner
				addHSolid(cx-armLen/2, cy-armLen/2, armLen, sThick)
				addVSolid(cx-armLen/2, cy-armLen/2, armLen, sThick)
			case 1: // top-right corner
				addHSolid(cx-armLen/2, cy-armLen/2, armLen, sThick)
				addVSolid(cx+armLen/2-sThick, cy-armLen/2, armLen, sThick)
			case 2: // bottom-left corner
				addHSolid(cx-armLen/2, cy+armLen/2-sThick, armLen, sThick)
				addVSolid(cx-armLen/2, cy-armLen/2, armLen, sThick)
			case 3: // bottom-right corner
				addHSolid(cx-armLen/2, cy+armLen/2-sThick, armLen, sThick)
				addVSolid(cx+armLen/2-sThick, cy-armLen/2, armLen, sThick)
			}

		case 1: // U-shape (3 walls, open on one side)
			bw := space * (0.7 + rand.Float64()*0.2)
			bh := space * (0.7 + rand.Float64()*0.2)
			cx := ox + (space-bw)/2
			cy := oy + (space-bh)/2
			switch rand.Intn(4) {
			case 0: // open top
				addHSolid(cx, cy+bh-sThick, bw, sThick)
				addVSolid(cx, cy, bh, sThick)
				addVSolid(cx+bw-sThick, cy, bh, sThick)
			case 1: // open bottom
				addHSolid(cx, cy, bw, sThick)
				addVSolid(cx, cy, bh, sThick)
				addVSolid(cx+bw-sThick, cy, bh, sThick)
			case 2: // open left
				addHSolid(cx, cy, bw, sThick)
				addHSolid(cx, cy+bh-sThick, bw, sThick)
				addVSolid(cx+bw-sThick, cy, bh, sThick)
			case 3: // open right
				addHSolid(cx, cy, bw, sThick)
				addHSolid(cx, cy+bh-sThick, bw, sThick)
				addVSolid(cx, cy, bh, sThick)
			}

		case 2: // Cross / plus shape (two crossing walls, open from all 4 quadrants)
			armLen := space * (0.7 + rand.Float64()*0.2)
			cx := ox + space/2
			cy := oy + space/2
			addHSolid(cx-armLen/2, cy-sThick/2, armLen, sThick)
			addVSolid(cx-sThick/2, cy-armLen/2, armLen, sThick)

		case 3: // Rectangular building with wide entrance
			bw := space * (0.6 + rand.Float64()*0.3)
			bh := space * (0.6 + rand.Float64()*0.3)
			cx := ox + (space-bw)/2
			cy := oy + (space-bh)/2
			sDoor := 120.0
			if sDoor > bw*0.6 {
				sDoor = bw * 0.6
			}
			if sDoor > bh*0.6 {
				sDoor = bh * 0.6
			}
			doorSide := rand.Intn(4)
			if doorSide == 0 {
				addHWall(cx, cy, bw, sThick, sDoor, false)
			} else {
				addHSolid(cx, cy, bw, sThick)
			}
			if doorSide == 1 {
				addHWall(cx, cy+bh-sThick, bw, sThick, sDoor, false)
			} else {
				addHSolid(cx, cy+bh-sThick, bw, sThick)
			}
			if doorSide == 2 {
				addVWall(cx, cy, bh, sThick, sDoor, false)
			} else {
				addVSolid(cx, cy, bh, sThick)
			}
			if doorSide == 3 {
				addVWall(cx+bw-sThick, cy, bh, sThick, sDoor, false)
			} else {
				addVSolid(cx+bw-sThick, cy, bh, sThick)
			}
		}

		placedCount++
		markUsed(gx, gy, 1, 1)
		credits -= 20
	}

	// ═══════════ Tier 3: Walls / barriers (1×1, cost 12) ═══════════
	for credits > budget*0.1+10 && placedCount < maxObjects {
		gx, gy, ok := getFreeGridCell(cols, rows, usedGrid)
		if !ok {
			break
		}
		wx, wy := gridCenter(gx, gy)

		switch rand.Intn(3) {
		case 0: // L-shaped wall
			arm := 60 + rand.Float64()*80
			addHSolid(wx-arm/2, wy, arm, 20)
			addVSolid(wx-arm/2, wy, arm, 20)
		case 1: // Straight wall
			wl := 80 + rand.Float64()*100
			if rand.Intn(2) == 0 {
				addHSolid(wx-wl/2, wy-10, wl, 20)
			} else {
				addVSolid(wx-10, wy-wl/2, wl, 20)
			}
		case 2: // Circle pillar
			r := 25 + rand.Float64()*40
			static.Circle = append(static.Circle, MapObject{X: wx, Y: wy, R: r})
		}

		placedCount++
		markUsed(gx, gy, 1, 1)
		credits -= 12
	}

	// ═══════════ Tier 4: Simple shapes (1×1, cost 5) ═══════════
	for credits >= 2 && placedCount < maxObjects {
		gx, gy, ok := getFreeGridCell(cols, rows, usedGrid)
		if !ok {
			break
		}
		wx, wy := gridCenter(gx, gy)

		switch rand.Intn(3) {
		case 0: // Circle
			r := 50 + rand.Float64()*50
			static.Circle = append(static.Circle, MapObject{X: wx, Y: wy, R: r})
		case 1: // Triangle
			s := 70 + rand.Float64()*40
			static.Poly = append(static.Poly, MapObject{Points: [][2]float64{
				{wx, wy - s/2},
				{wx + s/2, wy + s/2},
				{wx - s/2, wy + s/2},
			}})
		case 2: // Crate
			sz := 60 + rand.Float64()*30
			static.Rect = append(static.Rect, MapObject{X: wx - sz/2, Y: wy - sz/2, W: sz, H: sz})
		}

		placedCount++
		markUsed(gx, gy, 1, 1)
		credits -= 5
	}

	return static
}

// getFreeGridBlock finds a free contiguous block of bw×bh cells.
func getFreeGridBlock(cols, rows, bw, bh int, usedGrid [][]bool) (int, int, bool) {
	type pos struct{ x, y int }
	var candidates []pos
	for gx := 0; gx <= cols-bw; gx++ {
		for gy := 0; gy <= rows-bh; gy++ {
			free := true
			for dx := 0; dx < bw && free; dx++ {
				for dy := 0; dy < bh && free; dy++ {
					if usedGrid[gx+dx][gy+dy] {
						free = false
					}
				}
			}
			if free {
				candidates = append(candidates, pos{gx, gy})
			}
		}
	}
	if len(candidates) == 0 {
		return 0, 0, false
	}
	pick := candidates[rand.Intn(len(candidates))]
	return pick.x, pick.y, true
}

func getFreeGridCell(cols, rows int, usedGrid [][]bool) (int, int, bool) {
	return getFreeGridBlock(cols, rows, 1, 1, usedGrid)
}

// buildDynamicMap returns dynamic map objects that change each tick.
func buildDynamicMap(tick int) DynamicScene {
	dyn := DynamicScene{}
	// Process doors
	for _, d := range globalDoors {
		// Open and close cycle: 150 ticks closed, 150 ticks open
		cycle := (tick + d.Phase) % 300
		if cycle < 150 {
			// Closed - door is present as obstacle
			dyn.Rect = append(dyn.Rect, MapObject{X: d.X, Y: d.Y, W: d.W, H: d.H})
		}
	}
	return dyn
}

// --------------- Main ---------------

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: topdownshooter [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flagMapW := flag.Float64("w", 2000, "(float64) map width")
	flagMapH := flag.Float64("h", 2000, "(float64) map height")
	flagComplex := flag.Float64("complex", 0, "(float64) generate complex map (rooms, corridors, doors)")
	flagRoundSec := flag.Int("round-time", 60, "(int) round time in seconds")
	flagWinKills := flag.Int("win-kills", 30, "(int) kills needed to win")
	inputDir := flag.String("input-dir", "./bots/inputs", "(string) path to bots input directory")
	flag.Parse()

	MapW = *flagMapW
	MapH = *flagMapH
	RoundSec = *flagRoundSec
	WinKills = *flagWinKills
	maxTick := RoundSec * TPS

	fmt.Println("=== Top-Down Shooter ===")
	fmt.Printf("Map: %.0fx%.0f | %ds round | %d kills to win\n", MapW, MapH, RoundSec, WinKills)
	fmt.Printf("Viewer: http://localhost%s\n", WebPort)
	if lanIP := getLocalIP(); lanIP != "" {
		fmt.Printf("Network: http://%s%s\n", lanIP, WebPort)
	}
	fmt.Println()

	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html;charset=utf-8")
			fmt.Fprint(w, viewerHTML)
		})
		http.HandleFunc("/events", eventsEndpoint)
		log.Fatal(http.ListenAndServe(WebPort, nil))
	}()

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
	staticMap := generateMap(MapW, MapH, *flagComplex)

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
		pvs := make(map[string]PlayerView, len(players))
		for _, p := range players {
			pvs[p.ID] = playerToView(p)
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
				Players: players,
				Tick:    tick, MaxTick: maxTick,
				Kills: kills, Over: gameOver, Winner: winner,
				Scene: ViewScene{MapW: MapW, MapH: MapH, Static: staticMap, Dynamic: viewDyn},
			}
			data, _ := json.Marshal(vf)
			emit(data)
		}
	}

	// Final viewer frame
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
		Players: players,
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
		d        int
		avgPing  time.Duration
		timeouts int
	}
	lb := make([]lbEntry, 0, len(players))
	for _, p := range players {
		avg := time.Duration(0)
		if p.PingCount > 0 {
			avg = p.PingTotal / time.Duration(p.PingCount)
		}
		lb = append(lb, lbEntry{p.ID, p.Kills, p.Deaths, avg, p.Timeouts})
	}
	sort.Slice(lb, func(i, j int) bool { return lb[i].k > lb[j].k })
	for i, e := range lb {
		fmt.Printf("  #%d %s — %d kills / %d deaths | avg ping: %dµs | timeouts: %d\n",
			i+1, e.id, e.k, e.d, e.avgPing.Microseconds(), e.timeouts)
	}
	time.Sleep(5 * time.Second)
}

// getLocalIP returns the preferred outbound IPv4 address of the machine.
func getLocalIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
