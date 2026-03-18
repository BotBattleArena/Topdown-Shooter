- eig ganz cool das map daten jedes mal mitgeschickt werden, falls man battleroyal immer kleiner wird, aber brauch eig auch erst, wenn man wirklich battle royal funktionalität hat

- kills und deaths von players ist eig trotzdem interessant, falls man den schwächsten fokusen will

- bullets: lieber serversided dx, dy ausrechen, als das jeder bot das einzeln machen muss

- player color tatsächlich unnötig

- time left vllt trotzdem interessant, falls wenig time left und man strategie auf passiv wechseln will wenn man schon führt?

- tick fragwürdig, aber vllt wenn man längere calculation macht, um zu tracken wo man gerade ist im progress

- vllt die Game Rules & Constants am start des spiels übergeben?
    * Movement Speed: 3.5 units/tick.
    * Dash Speed: 14.0 units/tick for 8 ticks.
    * Bullet Speed: 10.0 units/tick.
    * Damage: 34 per hit (3 hits to kill).
    * Respawn Time: 120 ticks (2 seconds).
    * Shooting Cooldown: 25 ticks.
    * Dashing Cooldown: 180 ticks (3 seconds).
    * Win Condition: Reach 30 kills or have the most kills when the timer (60s) runs out.

-  gib vorher an, wieiviel tick pro sekunde läuft, damit man das auch in der bot logik berücksichtigen kann, bzw wieviel ms zwischen den ticks liegen

also so z.b.:
{
  "tps": 60,
  "tick_duration_ms": 16.66,
  "timeout_ms": 20,
  "rules": {
    "move_speed": 3.5,
    "bullet_speed": 10.0,
    "damage": 34,
    "win_kills": 30
  }
}
