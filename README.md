# zi

A simple text editor built referencing antirez's [kilo](https://github.com/antirez/kilo) but using `vim` style modal editing.

Does not use curses/ncurses and instead relies only ANSI escape sequences from the VT100 terminal. These codes are partially documented in `escape_codes.info`, but more detailed documentation can be found in the [VT100 reference manual](https://vt100.net/docs/vt100-ug/chapter3.html#S3.3.2).

Uses the [`termios` interface](http://man7.org/linux/man-pages/man3/termios.3.html) through unix specific golang bindings provided by https://github.com/golang/sys.
