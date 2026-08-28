# Gonex Pilot's Guide

The reentry-trader loop, from bar stool to burning sky and back. This is
the player-facing companion to [ARCHITECTURE.md](ARCHITECTURE.md); nothing
here is validated engineering — all of it is a game.

## The loop

Land → take contracts → outfit → launch → jump the lanes → land again.
Credits buy hulls, escorts and a power grid; the atmosphere charges you for
every tonne of it on the way back down.

## Where you land and take off

Landing is never a keypress — it is a descent you fly:

1. **Approach.** Fly within range of a planet in the top-down view; the
   banner reads *in approach range — L requests docking*.
2. **Handshake.** `L` hails control. After a beat you are assigned a pad
   and cleared, with a 30-second clearance window.
3. **Commit.** `L` again commits the deorbit burn. The planet swells, the
   first plasma washes the screen white, and you are on the corridor.
4. **The entry.** Fly the needle (below). Touch down inside 2 km of the
   pad line for the full pad bonus, inside 10 km for half.
5. **Docked.** The spaceport screen: bar, mission computer, trade,
   outfitter, shipyard, repairs, refuel — and the berth save.
6. **Takeoff.** `L` leaves the pad and puts you back in orbit over the
   flight map, escorts spawning on your wing.

Every touchdown costs a day; every jump costs three days and 100 fuel.

## Missions: the computer and the bar

Both screens run the recovered 1997 mission machine — 512 control bits and
the original mission records, GIVEN/SETS clauses intact.

- **Mission computer (`M`)** is the terminal. It lists *every* posting
  whose GIVEN clause matches this stellar **and your affiliations** — the
  control bits your past completions have set (and the blocking bits you
  have not tripped). Each row shows its pay and its **percentage chance**.
- **Spaceport bar (`B`)** is who actually walked in today. Once per
  landing, every eligible posting rolls a d100 against its `AvailRandom`
  percentage from the mission spec; the ones that pass are open for
  acceptance. The computer marks those rows **OPEN — press n**; the rest
  read *no slot today (nn% daily)*. Land again (or come back another day)
  and the dice roll fresh.

Accepting resolves the special stellar codes the 1997 data uses: `-4`
means "back where you accepted this", `10000+n` picks a random stellar of
government *n*. Cargo runs load at the origin or at the travel stop;
completion pays out on the return pad — unless your cargo clamps are above
50% damage, in which case the freight has spoiled on the way down.

## The steerable deflector cone

The Yodacon does not survive reentry by armor. It flies inside a
**magnetohydrodynamic deflector cone** — and the cone is why a 350-tonne
freighter with an anvil's ballistic coefficient can land at all.

**The ship is a bar magnet.** The nose coil makes the hull a polar dipole:
field lines emerge at the north pole ahead of the nose, loop around the
flanks, and re-enter at the tail —

    B ∝ [3(m·r̂)r̂ − m] / r³

Ionized flow is bound to those lines; its motion is the field-line
advection `dx/dτ = B(x)`. You can see the nested lobes around the hull in
the entry view, and every burning particle is riding one.

**The bow wave sets on fire.** Neutral air streams in from the horizon —
the direction you are flying — small with distance, swelling as it
arrives. At the standoff surface it shocks, ionizes, and *ignites*: from
that instant it is plasma, the dipole owns it, and it is deflected around
the ship instead of into it. Lithium seed (`[`/`]` feed) keeps the shock
layer ionized even when the air alone would recombine — **constant plasma
ignition**. That is the ride: as long as the sheath stays lit, the mirror
stays reflective, the heat pulse stays off the hull, and you are *riding
the plasma wave down* instead of being burned by it.

**Reflecting the energy back out.** The magnetopause stands off where
magnetic pressure balances ram pressure; the shielded heat flux falls with
the standoff (`q ∝ √(Rn/Reff)`). The cone is a one-way mirror: plasma
energy is reflected back out into the flow, not soaked. The GRIP the HUD
reports is the interaction parameter gating how much of the flow the
mirror actually commands.

**Steering with the cone.** The cone is steerable: rolling tilts the
dipole and hardens the mirror lobe on the side **opposite** the turn — the
deflected stream shoves the ship the other way. That asymmetric bright
lobe you see on the shell *is* the rudder. A coil **Boost** (`B`) doubles
the pattern into crossed quadrupole lobes for a few seconds of extra
authority, paid from the capacitors.

**The plasma colors** are the sheath read front-to-back: **blue** at the
bow where the flow first ignites, a **yellow** combustion band at the
mirror, **blue** again over the shoulders, then **pink → white → red** as
the deflected stream wraps aft along the field lines and recombines in the
wake.

## When the plasma lets go: aero steering

Steering authority is a handoff, and the HUD narrates it (`STEER PLASMA /
AERO`). The cone grips hardest at hypersonic speed in thin air; as the
ship slows the sheath cools, ionization dies, and MHD authority collapses.
By then the air is thick enough for the airframe itself — control surfaces
on dynamic pressure. When aero passes plasma the console makes the
dropship call: **"We're in the pipe, five by five."** From there you are
flying an aircraft: shorelines slide beneath you, city lights resolve out
of the haze and twinkle past, and the pad marker comes up on final.

## Scale: five decades in one camera

Interface is at 122 km; the pad is a 2 km line. The landing camera spans
those five orders of magnitude by thinking in **log-altitude tiers**: the
planet limb first, then shore patterns (~60 km), city lights (~45 km),
terrain grain (~30 km), the pad itself on final — each tier fading in over
about a third of a decade. All simulation math runs in `float64`, Go's
native word, end to end; coordinates collapse to `float32` only at the
draw call, which is the renderer's fast path. The particle inner loop is
trig-free — one square root per bounce, straight per-channel lerps for
color.

## The landing site: a seed-grown spaceport

Every stellar grows its own port town from its seed — same planet, same
town, forever — using the ascitty seed-world construction (generated
street axes with a road hierarchy, blocks split into lots, building
heights from a falloff-plus-noise field drawn through a skewed four-band
distribution, a narrow district palette, windows hashed per lot/floor/bay,
never stored). The placement is restricted for a port: the whole town
sits on ONE side of the **spaceport road** — an arterial nearly a hundred
metres kerb to kerb, laid like a runway with a centreline, threshold bars
and edge lamps. That road is the pad line: crossrange zero is its
centreline, and you land on it. It carries no traffic and no buildings —
the cars and the people stay on the town's own avenues and frontages.
The town is always on land: past the shore line the water starts,
glinting, and the touchdown line never crosses it.

From altitude the world is settled the same way: the small towns you
overfly are connected — faint highways run from each to the next.

**You land and take off next to the same city.** Below about 18 km the
chase camera eases back just far enough to hold the port in frame, so the
whole terminal sink happens beside the town — the same seed, the same
skyline, the same side of the road — and the departure roll opens on the
view the landing ended with.

## The console cluster

The entry screen carries the reentry-console instrument set while the
main view stays on the visualization: the top telemetry strip (T,
altitude, velocity, Mach, stagnation flux, wall temp, decel, interaction
Q, electron density, ship power), the **dial cluster** — G-load, flux
against limit, wall temperature, Mach, standoff, hull soak, battery,
lithium, each a 270° gauge with its red zone marked and its needle live —
and the **ORBIT & APPROACH** card: the planet disc, the parking orbit you
left, and your descent spiralling in, altitudes exaggerated so the
trajectory reads.

## Takeoff: the landing, backwards, and quicker

`L` on the pad rolls you down the spaceport road like an airport
departure: throttle up between the edge lamps, rotate, climb out over the
town, and watch the sky run the entry's decades in reverse — powder blue
thinning to indigo, the ionosphere ribbon, then black, stars, the nebula
— ending in orbit on the flight map, escorts on the wing. Twelve seconds
against the entry's hundred.

## The clock

Landings and takeoffs run the sun's clock fast: each one crosses at
least a quarter of a day-night cycle, so a descent that begins in
daylight can flare out over a town that has turned its lights on.

## The spaceport screens

| Key | Screen | What it does |
|-----|--------|--------------|
| `B` | Spaceport bar | accept today's rolled contracts; hire (`H`)/fire (`F`) **escorts**, sign (`C`)/dismiss (`X`) **crew** |
| `M` | Mission computer | every posting for this port + your affiliations, with the daily odds |
| `T` | Trade center | buy/sell cargo (`+`/`-`) |
| `O` | Outfitter | generators, batteries, capacitors, radiators — mass with consequences |
| `S` | Shipyard | browse every hull (`↑`/`↓`), `Enter` buys at 60% trade-in |
| `R` | Refuel (hold) | jump fuel first, then lithium for the shield |
| `Y` | Yard repairs | clears hull / computer / clamp damage |
| `V` | Save berth | write the pilot file by hand |
| `L` | Leave | launch to orbit, escorts on the wing |

Escorts cost a hire fee plus a daily wage; crew wage is smaller. Payroll
walks on every jump and landing — an empty ledger means a thirsty crew,
not a mutiny. Yet.

## Saving, and DED

**Every touchdown writes the berth save automatically** (`save.json` — the
world, the voyage, the bits, the payroll, everything), and `V` on the pad
writes one on demand. The Escape-menu Save/Load still work anywhere.

When the corridor wins — hull to 100% in the plasma, or arriving at the
deck too hot — the screen goes dark and says, in twelve-times letters:

    DED

Any key resumes from the last berth save: the last pad you stood on, the
credits you had then, the missions you were flying then. What happened
above that other sky stays there.
