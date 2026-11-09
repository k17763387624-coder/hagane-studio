//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include "window_darwin.h"

static BOOL HWIsLocalURL(NSURL *base, NSURL *candidate) {
    return base && candidate && !candidate.user && !candidate.password &&
        [base.scheme.lowercaseString isEqualToString:candidate.scheme.lowercaseString] &&
        [base.host.lowercaseString isEqualToString:candidate.host.lowercaseString] &&
        ((base.port == nil && candidate.port == nil) || [base.port isEqualToNumber:candidate.port]);
}

static BOOL HWIsExternalURL(NSURL *url) {
    return url.host.length > 0 && !url.user && !url.password &&
        ([url.scheme.lowercaseString isEqualToString:@"http"] || [url.scheme.lowercaseString isEqualToString:@"https"]);
}

@interface HWHaganeWindow : NSObject <NSWindowDelegate, NSApplicationDelegate, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler, WKDownloadDelegate>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) WKWebView *view;
@property(nonatomic, strong) NSURL *baseURL;
@property(nonatomic, strong) NSMutableArray<HWHaganeWindow *> *children;
@property(nonatomic, weak) HWHaganeWindow *parent;
@property(nonatomic) BOOL closing;
@property(nonatomic) BOOL awaitingAnswer;
@property(nonatomic) BOOL showingQuestion;
@property(nonatomic) NSUInteger question;
- (instancetype)initWithURL:(NSURL *)url configuration:(WKWebViewConfiguration *)configuration parent:(HWHaganeWindow *)parent;
- (void)requestClose;
- (void)forceClose;
- (void)show;
@end

static HWHaganeWindow *mainWindow;

@implementation HWHaganeWindow
- (instancetype)initWithURL:(NSURL *)url configuration:(WKWebViewConfiguration *)configuration parent:(HWHaganeWindow *)parent {
    self = [super init];
    if (!self) return nil;
    self.baseURL = url;
    self.parent = parent;
    self.children = [NSMutableArray array];
    WKUserContentController *content = [[WKUserContentController alloc] init];
    [content addScriptMessageHandler:self name:@"haganeWorks"];
    NSString *bridge = @"window.haganeWorks = Object.freeze({open: (url) => window.webkit.messageHandlers.haganeWorks.postMessage({open: String(url)}), close: (answer) => window.webkit.messageHandlers.haganeWorks.postMessage({close: String(answer)})});";
    [content addUserScript:[[WKUserScript alloc] initWithSource:bridge injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:YES]];
    configuration.userContentController = content;
    configuration.preferences.javaScriptCanOpenWindowsAutomatically = YES;
    self.view = [[WKWebView alloc] initWithFrame:NSZeroRect configuration:configuration];
    self.view.navigationDelegate = self;
    self.view.UIDelegate = self;
    NSRect screen = NSScreen.mainScreen.visibleFrame;
    NSRect frame = NSMakeRect(0, 0, MIN(1500, screen.size.width * 0.92), MIN(1000, screen.size.height * 0.9));
    self.window = [[NSWindow alloc] initWithContentRect:frame styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
    self.window.title = @"Hagane Works";
    self.window.minSize = NSMakeSize(720, 480);
    self.window.releasedWhenClosed = NO;
    self.window.delegate = self;
    self.window.contentView = self.view;
    [self.window center];
    [self show];
    return self;
}

- (void)show {
    if (self.closing) return;
    if (self.window.miniaturized) [self.window deminiaturize:nil];
    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}

- (BOOL)windowShouldClose:(NSWindow *)sender {
    if (self.closing) return YES;
    [self requestClose];
    return NO;
}

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    [self requestClose];
    return NSTerminateCancel;
}

- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)visible {
    [self show];
    return YES;
}

- (void)requestClose {
    if (self.closing || self.awaitingAnswer) return;
    self.awaitingAnswer = YES;
    self.showingQuestion = NO;
    NSUInteger question = ++self.question;
    [self.view evaluateJavaScript:@"typeof window.haganeWorksCloseRequested === 'function' ? window.haganeWorksCloseRequested() : window.haganeWorks.close('close');" completionHandler:nil];
    __weak HWHaganeWindow *weakSelf = self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
        HWHaganeWindow *strongSelf = weakSelf;
        if (strongSelf && strongSelf.awaitingAnswer && !strongSelf.showingQuestion && strongSelf.question == question) [strongSelf forceClose];
    });
}

- (void)forceClose {
    if (self.closing) return;
    self.closing = YES;
    self.awaitingAnswer = NO;
    for (HWHaganeWindow *child in self.children.copy) [child forceClose];
    [self.window close];
}

- (void)windowWillClose:(NSNotification *)notification {
    self.closing = YES;
    [self.view.configuration.userContentController removeScriptMessageHandlerForName:@"haganeWorks"];
    if (!self.parent) {
        [NSApp stop:nil];
        NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];
        [NSApp postEvent:wake atStart:YES];
    } else {
        [self.parent.children removeObject:self];
    }
}

- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
    if (!message.frameInfo.mainFrame || !HWIsLocalURL(self.baseURL, message.frameInfo.request.URL) || ![message.body isKindOfClass:NSDictionary.class]) return;
    NSDictionary *body = message.body;
    NSString *address = [body[@"open"] isKindOfClass:NSString.class] ? body[@"open"] : nil;
    NSURL *url = address ? [NSURL URLWithString:address] : nil;
    if (HWIsExternalURL(url) && !HWIsLocalURL(self.baseURL, url)) [NSWorkspace.sharedWorkspace openURL:url];
    NSString *answer = [body[@"close"] isKindOfClass:NSString.class] ? body[@"close"] : nil;
    if (!self.awaitingAnswer || !answer) return;
    if ([answer isEqualToString:@"close"]) [self forceClose];
    else if ([answer isEqualToString:@"asking"]) { self.showingQuestion = YES; [self show]; }
    else if ([answer isEqualToString:@"stay"]) { self.awaitingAnswer = NO; self.showingQuestion = NO; }
}

- (BOOL)allowsURL:(NSURL *)url {
    if (HWIsLocalURL(self.baseURL, url)) return YES;
    if ([url.absoluteString isEqualToString:@"about:blank"]) return YES;
    if ([url.scheme.lowercaseString isEqualToString:@"blob"]) return HWIsLocalURL(self.baseURL, [NSURL URLWithString:[url.absoluteString substringFromIndex:5]]);
    return NO;
}

- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    NSURL *url = action.request.URL;
    if ([self allowsURL:url]) {
        decisionHandler(action.shouldPerformDownload ? WKNavigationActionPolicyDownload : WKNavigationActionPolicyAllow);
    } else {
        if (HWIsExternalURL(url)) [NSWorkspace.sharedWorkspace openURL:url];
        decisionHandler(WKNavigationActionPolicyCancel);
    }
}

- (void)webView:(WKWebView *)webView decidePolicyForNavigationResponse:(WKNavigationResponse *)response decisionHandler:(void (^)(WKNavigationResponsePolicy))decisionHandler {
    NSString *disposition = [(NSHTTPURLResponse *)response.response isKindOfClass:NSHTTPURLResponse.class] ? [(NSHTTPURLResponse *)response.response allHeaderFields][@"Content-Disposition"] : nil;
    BOOL attachment = [disposition.lowercaseString hasPrefix:@"attachment"];
    decisionHandler(response.canShowMIMEType && !attachment ? WKNavigationResponsePolicyAllow : WKNavigationResponsePolicyDownload);
}

- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features {
    if (action.targetFrame != nil) return nil;
    if (![self allowsURL:action.request.URL]) {
        if (HWIsExternalURL(action.request.URL)) [NSWorkspace.sharedWorkspace openURL:action.request.URL];
        return nil;
    }
    HWHaganeWindow *child = [[HWHaganeWindow alloc] initWithURL:self.baseURL configuration:configuration parent:self];
    [self.children addObject:child];
    return child.view;
}

- (void)webViewDidClose:(WKWebView *)webView { [self requestClose]; }

- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
    self.awaitingAnswer = NO;
    self.showingQuestion = NO;
    self.question++;
    [webView evaluateJavaScript:@"document.title" completionHandler:^(id title, NSError *error) {
        if ([title isKindOfClass:NSString.class] && [title length] > 0) self.window.title = title;
    }];
}

- (void)webView:(WKWebView *)webView runOpenPanelWithParameters:(WKOpenPanelParameters *)parameters initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(NSArray<NSURL *> *))completionHandler {
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    panel.allowsMultipleSelection = parameters.allowsMultipleSelection;
    panel.canChooseDirectories = parameters.allowsDirectories;
    panel.canChooseFiles = YES;
    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) {
        completionHandler(result == NSModalResponseOK ? panel.URLs : nil);
    }];
}

- (void)webView:(WKWebView *)webView runJavaScriptAlertPanelWithMessage:(NSString *)message initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(void))completionHandler {
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"Hagane Works";
    alert.informativeText = message;
    [alert addButtonWithTitle:NSLocalizedString(@"OK", nil)];
    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) { completionHandler(); }];
}

- (void)webView:(WKWebView *)webView runJavaScriptConfirmPanelWithMessage:(NSString *)message initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(BOOL))completionHandler {
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"Hagane Works";
    alert.informativeText = message;
    [alert addButtonWithTitle:NSLocalizedString(@"OK", nil)];
    [alert addButtonWithTitle:NSLocalizedString(@"Cancel", nil)];
    [alert beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) { completionHandler(result == NSAlertFirstButtonReturn); }];
}

- (void)webView:(WKWebView *)webView navigationAction:(WKNavigationAction *)action didBecomeDownload:(WKDownload *)download { download.delegate = self; }
- (void)webView:(WKWebView *)webView navigationResponse:(WKNavigationResponse *)response didBecomeDownload:(WKDownload *)download { download.delegate = self; }
- (void)download:(WKDownload *)download decideDestinationUsingResponse:(NSURLResponse *)response suggestedFilename:(NSString *)filename completionHandler:(void (^)(NSURL *))completionHandler {
    NSSavePanel *panel = [NSSavePanel savePanel];
    panel.nameFieldStringValue = filename;
    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result) { completionHandler(result == NSModalResponseOK ? panel.URL : nil); }];
}
@end

static void HWBuildMenu(void) {
    NSMenu *menu = [[NSMenu alloc] init];
    NSMenuItem *appItem = [[NSMenuItem alloc] init];
    NSMenu *app = [[NSMenu alloc] initWithTitle:@"Hagane Works"];
    [app addItemWithTitle:NSLocalizedString(@"About Hagane Works", nil) action:@selector(orderFrontStandardAboutPanel:) keyEquivalent:@""];
    [app addItem:NSMenuItem.separatorItem];
    [app addItemWithTitle:NSLocalizedString(@"Hide Hagane Works", nil) action:@selector(hide:) keyEquivalent:@"h"];
    [app addItemWithTitle:NSLocalizedString(@"Quit Hagane Works", nil) action:@selector(terminate:) keyEquivalent:@"q"];
    appItem.submenu = app;
    [menu addItem:appItem];
    NSMenuItem *editItem = [[NSMenuItem alloc] init];
    NSMenu *edit = [[NSMenu alloc] initWithTitle:NSLocalizedString(@"Edit", nil)];
    [edit addItemWithTitle:NSLocalizedString(@"Undo", nil) action:@selector(undo:) keyEquivalent:@"z"];
    NSMenuItem *redo = [edit addItemWithTitle:NSLocalizedString(@"Redo", nil) action:@selector(redo:) keyEquivalent:@"Z"];
    redo.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagShift;
    [edit addItem:NSMenuItem.separatorItem];
    [edit addItemWithTitle:NSLocalizedString(@"Cut", nil) action:@selector(cut:) keyEquivalent:@"x"];
    [edit addItemWithTitle:NSLocalizedString(@"Copy", nil) action:@selector(copy:) keyEquivalent:@"c"];
    [edit addItemWithTitle:NSLocalizedString(@"Paste", nil) action:@selector(paste:) keyEquivalent:@"v"];
    [edit addItemWithTitle:NSLocalizedString(@"Select All", nil) action:@selector(selectAll:) keyEquivalent:@"a"];
    editItem.submenu = edit;
    [menu addItem:editItem];
    NSMenuItem *windowItem = [[NSMenuItem alloc] init];
    NSMenu *window = [[NSMenu alloc] initWithTitle:NSLocalizedString(@"Window", nil)];
    [window addItemWithTitle:NSLocalizedString(@"Minimize", nil) action:@selector(performMiniaturize:) keyEquivalent:@"m"];
    [window addItemWithTitle:NSLocalizedString(@"Close", nil) action:@selector(performClose:) keyEquivalent:@"w"];
    windowItem.submenu = window;
    [menu addItem:windowItem];
    NSApp.mainMenu = menu;
    NSApp.windowsMenu = window;
}

int hw_window_create(const char *address) {
    @autoreleasepool {
        if (![NSThread isMainThread] || mainWindow) return 0;
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        HWBuildMenu();
        NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:address]];
        mainWindow = [[HWHaganeWindow alloc] initWithURL:url configuration:[[WKWebViewConfiguration alloc] init] parent:nil];
        NSApp.delegate = mainWindow;
        [mainWindow.view loadRequest:[NSURLRequest requestWithURL:url]];
        return mainWindow ? 1 : 0;
    }
}

void hw_window_run(void) { @autoreleasepool { [NSApp run]; } }
void hw_window_close(void) { dispatch_async(dispatch_get_main_queue(), ^{ [mainWindow forceClose]; }); }
void hw_window_show(void) { dispatch_async(dispatch_get_main_queue(), ^{ [mainWindow show]; }); }
int hw_window_navigation_is_local(const char *base, const char *candidate) {
    @autoreleasepool {
        return HWIsLocalURL([NSURL URLWithString:[NSString stringWithUTF8String:base]], [NSURL URLWithString:[NSString stringWithUTF8String:candidate]]) ? 1 : 0;
    }
}
