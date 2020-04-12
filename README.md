# zi

A simple text editor built referencing antirez's [kilo](https://github.com/antirez/kilo).

Does not use curses/ncurses and instead relies only ANSI escape sequences from the VT100 terminal. These codes are partially documented in `escape_codes.info`, but more detailed documentation can be found in the VT100 references manual: https://vt100.net/docs/vt100-ug/chapter3.html#S3.3.2
