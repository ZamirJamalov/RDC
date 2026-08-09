// PR #144: Shared auth helper — intercepts all fetch calls to /api/* and adds Bearer token.
// Token is stored in localStorage after login, alongside the httpOnly cookie.
// This dual approach (cookie + Bearer) ensures auth works even if cookie has issues.
(function() {
    const originalFetch = window.fetch;
    window.fetch = function(url, options) {
        options = options || {};
        // Normalize URL to string
        const urlStr = typeof url === 'string' ? url : (url && url.url ? url.url : String(url));
        // Only add auth header for /api/ paths (not for static files)
        if (urlStr && urlStr.startsWith('/api/')) {
            const token = localStorage.getItem('rdc_token');
            if (token) {
                options.headers = options.headers || {};
                // Don't override existing Authorization header
                if (!options.headers['Authorization'] && !options.headers['authorization']) {
                    options.headers['Authorization'] = 'Bearer ' + token;
                }
            }
        }
        // Always include credentials (cookies) for same-origin requests
        if (!options.credentials) {
            options.credentials = 'same-origin';
        }
        return originalFetch(url, options);
    };

    // Helper: store token after login
    window.setAuthToken = function(token) {
        localStorage.setItem('rdc_token', token);
    };

    // Helper: clear token on logout
    window.clearAuthToken = function() {
        localStorage.removeItem('rdc_token');
    };

    // Helper: check if token exists
    window.hasAuthToken = function() {
        return !!localStorage.getItem('rdc_token');
    };
})();
