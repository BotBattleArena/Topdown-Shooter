# Bot Protocol: Top-Down Shooter

This document describes the communication protocol between the Top-Down Shooter server and the bots.

## Overview

The game runs at **60 Ticks Per Second (TPS)**. In each tick, the server sends the current game state to all bots and expects a set of "Axes" (inputs) in return.

- **Deadline**: Bots must respond within **12ms** to maintain a stable 60 TPS.
- **Format**: JSON.

---

## Phase 0: Initialization

Before the first game tick, the server sends a setup message. Bots should use this to configure their internal constants.

```json
{
  "type": "setup",
  "tps": 60,
  "tick_duration_ms": 16.666,
  "timeout_ms": 12,
  "rules": {
    "map_w": 2000,
    "map_h": 2000,
    "move_speed": 3.5,
    "bullet_speed": 10.0,
    "damage": 34,
    "win_kills": 30,
    "respawn_ticks": 120,
    "shoot_cd": 25,
    "dash_cd": 180
  }
}
```

---

## Phase 1: Game Loop (`BotFrame`)

Each tick, the server sends a JSON object representing the world state.

### Message Structure

```json
{
  "players": {
    "bot_id_1": {
      "id": "bot_id_1",
      "x": 100.5,
      "y": 200.0,
      "aim_x": 0.707,
      "aim_y": 0.707,
      "hp": 100,
      "kills": 5,
      "deaths": 2,
      "alive": true,
      "shoot_cd": 10,
      "dash_cd": 120,
      "color": "#e74c3c"
    }
    // ... other players
  },
  "bullets": [
    {
      "x": 150.0,
      "y": 160.0,
      "dx": 1.0,
      "dy": 0.0,
      "owner": "bot_id_2"
    }
  ],
  "map_w": 2000,
  "map_h": 2000,
  "tick": 1234,
  "time_left": 45
}
```

### Field Definitions

| Field | Type | Description |
| :--- | :--- | :--- |
| `players` | `Map<String, Player>` | Map of all players in the game, keyed by their ID. |
| `bullets` | `Array<Bullet>` | List of active bullets in the air. |
| `map_w` / `map_h` | `Float` | Dimensions of the map. |
| `tick` | `Int` | Current game tick. |
| `time_left` | `Int` | Seconds remaining in the round. |

#### Player Object
- `x`, `y`: Current position.
- `aim_x`, `aim_y`: Current normalized aiming direction.
- `hp`: Health points (0-100).
- `shoot_cd`: Ticks until the bot can shoot again (60 TPS).
- `dash_cd`: Ticks until the bot can dash again.

---

## Bot to Server: `Axes`

The bot must respond with a JSON object mapping axis names to numeric values.

### Expected Response Structure

```json
{
  "move_x": 0.0,
  "move_y": 0.0,
  "aim_x": 1.0,
  "aim_y": 0.0,
  "shoot": 0,
  "dash": 0
}
```

### Axis Definitions

| Axis | Range | Description |
| :--- | :--- | :--- |
| `move_x` | `[-1.0, 1.0]` | Movement on the X axis. |
| `move_y` | `[-1.0, 1.0]` | Movement on the Y axis. |
| `aim_x` | `[-1.0, 1.0]` | Aiming direction X. |
| `aim_y` | `[-1.0, 1.0]` | Aiming direction Y. |
| `shoot` | `> 0` | Triggers a shot if `shoot_cd` is 0. |
| `dash` | `> 0` | Triggers a dash in the aiming direction if `dash_cd` is 0. |

---

## Game Rules & Constants

- **Movement Speed**: 3.5 units/tick.
- **Dash Speed**: 14.0 units/tick for 8 ticks.
- **Bullet Speed**: 10.0 units/tick.
- **Damage**: 34 per hit (3 hits to kill).
- **Respawn Time**: 120 ticks (2 seconds).
- **Shooting Cooldown**: 25 ticks.
- **Dashing Cooldown**: 180 ticks (3 seconds).
- **Win Condition**: Reach 30 kills or have the most kills when the timer (60s) runs out.
