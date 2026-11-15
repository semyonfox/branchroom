import {rankPaletteResults} from './command-palette.ts';
import type {PaletteResult} from './command-palette.ts';

test('filters every word and ranks exact matches before prefixes and paths without mutating results', () => {
  const results: PaletteResult[] = ['src/tides.go', 'Tides guide', 'tides', 'other.go', 'Tides calendar'].map((label) => ({label, section: 'files'}));
  expect(rankPaletteResults(results, ' TIDES ').map(({label}) => label)).toEqual(['tides', 'Tides guide', 'Tides calendar', 'src/tides.go']);
  expect(rankPaletteResults(results, 'tides guide').map(({label}) => label)).toEqual(['Tides guide']);
  expect(rankPaletteResults(results, 'missing')).toEqual([]);
  expect(rankPaletteResults(results, '')).toEqual(results);
  expect(results[0].label).toBe('src/tides.go');
});
