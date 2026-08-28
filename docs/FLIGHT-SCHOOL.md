# The Approach School — the 2026 branch of the 1997 academy

ConEx 1.2 shipped with a flight school: missions 250–281 at the ConEx
spaceport, chained on control bits 121→152 — Flight Practice, Freight,
Shipping 101/202, Trading 101/202, then Start Combat Training and a long
combat ladder. Gonex runs that original chain unmodified.

The Approach School (missions 300–309, bits 170→179) forks the chain at
**bit 126 — after Trading 202, *instead of* Start Combat Training**. That
fork is the design thesis: the 2026 game advances the pilot through the
environment, not through fights. Same bar, same bit machine, same special
stellar codes (−4 "back where you took it", 10000+n "a random
Confederation stellar") the 1997 records use.

## The syllabus

| # | Mission | Teaches | Mechanic under test |
|---|---------|---------|---------------------|
| 300 | The Corridor | fly the needle | corridor band, steep/shallow, pad line |
| 301 | Ride the Pillow | heat is the currency | lithium feed, FLUX/WALL dials, battery |
| 302 | The Handoff | five by five | plasma→aero authority, glide 5:5, RCS budget |
| 303 | Hot and Steep | the wager | guardian, hull scrub, sonic-boom fine vs pad bonus |
| 304 | Doctrine: The Courier | Dart '97 (700 vel, 1.6 kt) | light+fast = cool corridor, deadline runs |
| 305 | Doctrine: The Pins | pins (90–850 t) | low mass = forgiving entries, new-planet hull |
| 306 | Doctrine: The Hauler | Tomaquad '97 (300 vel) | heavy = hot corridor; margin-per-tonne math |
| 307 | Doctrine: The Escort Wing | Gryphon/Defender '97 | hiring at the bar, wages as a position |
| 308 | Doctrine: The Freighter | Yodacon '97 (600 vel, 350 t) | everything at once, on the line |
| 309 | Rags to Riches | the board | market events, buy low / sell high, seed capital |

Lessons 300–303 are take-off-and-land-again rounds at ConEx (travel −1,
return −4). The doctrine runs use the 1997 random-Confederation code
(travel 10000): couriers and wings come home (return −4), deliveries are
one-way (return −1). Each lesson blocks on its own completion bit, so the
board offers exactly the next open lesson.

## Why a ship lesson plan

The catalog's `specs.xml` numbers are konex's own data, and they are the
curriculum: **velocity** is deadline reach, **mass** is what the
atmosphere charges on entry (ballistic coefficient → corridor heat),
**acceleration/turn** is pad work and escort utility, **damage** is what
you can shrug off while running. A commodity captain picks the hull whose
numbers price the run best — the school walks that table one hull at a
time, ending on the Yodacon, whose 350 tonnes are the same 350 tonnes the
reentry envelope model was built around.
