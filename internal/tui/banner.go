package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// bannerBitmap is VGXNESS in a 5×7 pixel font with two blank columns between
// letters (47 columns), taken from the canvas artboard. Pairs of pixel rows
// become one terminal row of half blocks, so the banner is four rows tall.
var bannerBitmap = [7]string{
	"#...#...###...#...#..#...#..#####...####...####",
	"#...#..#...#...#.#...##..#..#......#......#....",
	"#...#..#........#....#.#.#..#......#......#....",
	"#...#..#.###....#....#..##..####....###....###.",
	".#.#...#...#....#....#...#..#..........#......#",
	".#.#...#...#...#.#...#...#..#..........#......#",
	"..#.....###...#...#..#...#..#####..####...####.",
}

const bannerWidth = 47

func banner() []string {
	rows := (len(bannerBitmap) + 1) / 2
	gradient := lipgloss.Blend1D(rows, colorAccent, colorAccentStrong)
	lines := make([]string, rows)
	for row := range rows {
		var line strings.Builder
		for column := range bannerWidth {
			top := bannerBitmap[row*2][column] == '#'
			bottom := row*2+1 < len(bannerBitmap) && bannerBitmap[row*2+1][column] == '#'
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top:
				line.WriteRune('▀')
			case bottom:
				line.WriteRune('▄')
			default:
				line.WriteRune(' ')
			}
		}
		lines[row] = lipgloss.NewStyle().Foreground(gradient[row]).Bold(true).Render(line.String())
	}
	return lines
}
