package main

// USAtlanticAndMediterraneanBoundingBox defines the geographic bounding box
// covering a large portion of the western Atlantic Ocean (including the U.S. East Coast),
// and extending eastward to include the entire Mediterranean Sea region.
//
// This bounding box is represented as a two-point diagonal:
// [ [southWestLat, southWestLon], [northEastLat, northEastLon] ]
//
// Typical use cases include: filtering AIS data to ships within this maritime domain.
var USAtlanticAndMediterraneanBoundingBox = [][][]float64{
	{
		{4.767311413839607, -102.0289629925216}, // South-West corner (latitude, longitude)
		{46.46813850956005, 39.55708947212315},  // North-East corner (latitude, longitude)
	},
}

// AtlanticAndMediterraneanBoundingBox defines a bounding box for the Eastern Atlantic,
// including North Africa, Iberian Peninsula, and the full Mediterranean Basin.
//
// This region is often used for geospatial subsetting of maritime data (e.g., AIS),
// where the focus is on commercial traffic between Europe, Africa, and the Middle East.
var AtlanticAndMediterraneanBoundingBox = [][][]float64{
	{
		{25.076647686560193, -21.45976982501989}, // South-West corner (latitude, longitude)
		{47.955969745737974, 44.99448027942182},  // North-East corner (latitude, longitude)
	},
}

// GlobalBoundingBox represents the full latitude-longitude extent of the Earth.
// It covers all oceanic and terrestrial areas, making it suitable as a default
// global filter or fallback when no spatial constraints are applied.
//
// Format: [ [southWestLat, southWestLon], [northEastLat, northEastLon] ]
var GlobalBoundingBox = [][][]float64{
	{
		{-90.0, -180.0}, // Absolute minimum (SW corner of globe)
		{90.0, 180.0},   // Absolute maximum (NE corner of globe)
	},
}
