function resolveBaseURL(testInfo, override) {
  if (override) {
    return override;
  }

  const projectBase = testInfo?.project?.use?.baseURL;
  const configBase = testInfo?.config?.use?.baseURL;
  const envBase = process.env.PLAYWRIGHT_BASE_URL;

  return projectBase || configBase || envBase || 'http://localhost:8080';
}

module.exports = {
  resolveBaseURL,
};
