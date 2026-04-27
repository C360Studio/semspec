package webingest

// sampleHTML is shared across tests in this package. Mirrors the fixture in
// tools/httptool/format_test.go so converter behaviour can be verified
// against the same content the agent-facing layer sees.
const sampleHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Sample Page</title>
  <meta name="description" content="A page about widgets and gadgets.">
</head>
<body>
  <nav><a href="/home">Home</a></nav>
  <article>
    <h1>Widgets and Gadgets</h1>
    <p>Widgets are small things. Gadgets are clever.</p>
    <h2>Installing widgets</h2>
    <p>Run <code>npm install widget</code> and you're done.</p>
    <h2>Using widgets</h2>
    <p>Call <a href="/api/widgets">the widget API</a>.</p>
    <h3>Advanced</h3>
    <p>For advanced use cases see <a href="https://example.com/advanced">here</a>.</p>
  </article>
  <footer><a href="javascript:void(0)">Click me</a></footer>
</body>
</html>`
