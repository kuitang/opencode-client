async function injectSessionHelper(page) {
  await page.addInitScript(() => {
    (function sessionHelper() {
      const originalFetch = window.fetch;

      window.fetch = function patchedFetch(...args) {
        return originalFetch.apply(this, args).then((response) => {
          const url = args[0];
          if ((url === '/send' || url === '/') && response && response.ok) {
            try {
              window.localStorage.setItem('hasActiveSession', 'true');
            } catch (error) {
              console.warn('[Session Helper] Failed to persist session marker', error);
            }
          }
          return response;
        });
      };

      if (window.localStorage.getItem('hasActiveSession') === 'true') {
        document.cookie = 'session=playwright_restored; path=/';
      }
    })();
  });
}

module.exports = {
  injectSessionHelper,
};
