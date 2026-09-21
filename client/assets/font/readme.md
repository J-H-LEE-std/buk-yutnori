# Korean UI fonts

These are static, subsetted Noto Sans KR fonts (weights 400 and 700),
distributed under the SIL Open Font License in [ofl.txt](ofl.txt).
They replace the original four-byte placeholder files.

Source: https://github.com/google/fonts/tree/main/ofl/notosanskr
Downloaded 2026-09-20: `NotoSansKR[wght].ttf`.

Reproduce with FontTools, for each weight (400 / 700):

```sh
fonttools varLib.instancer 'NotoSansKR[wght].ttf' wght=400 --output=regular.ttf
pyftsubset regular.ttf --unicodes='U+0000-00FF,U+1100-11FF,U+2000-206F,U+3000-318F,U+AC00-D7AF,U+5317' --output-file=notosans_kr_regular.ttf
```

The subset includes modern Hangul syllables and jamo, Latin, punctuation,
and the 北 brand glyph. Other scripts use the browser's fallback fonts.
