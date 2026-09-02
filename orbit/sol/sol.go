// Package sol holds real-world constants for games set in our solar
// system. The orbit package itself is unit-agnostic and works with any
// masses, any gravitational constant and any scale of distance and time,
// so a fictional system needs nothing from this package.
package sol

// Distances in metres, masses in kilograms, time in seconds.
const (
	AU          = 1.495978707e11
	Day         = 86400.0
	Year        = 365.25 * Day
	EarthRadius = 6.371e6
	MoonRadius  = 1.7374e6
	SunRadius   = 6.957e8

	SunMass     = 1.98892e30
	EarthMass   = 5.9722e24
	MoonMass    = 7.342e22
	MarsMass    = 6.4171e23
	JupiterMass = 1.8982e27

	// Standard gravitational parameters (G times mass), m³/s².
	MuSun     = 1.32712440018e20
	MuEarth   = 3.986004418e14
	MuMoon    = 4.9048695e12
	MuMars    = 4.282837e13
	MuJupiter = 1.26686534e17

	MoonDistance = 384400e3
)
