#ifndef HAGANE_WINDOW_DARWIN_H
#define HAGANE_WINDOW_DARWIN_H

int hw_window_create(const char *address);
void hw_window_run(void);
void hw_window_close(void);
void hw_window_show(void);
int hw_window_navigation_is_local(const char *base, const char *candidate);

#endif
