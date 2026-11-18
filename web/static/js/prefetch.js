// Loads every module and language catalog of the site into the browser cache
// once this page has loaded, so later pages and a language switch need no
// request. The server lists versioned URLs in <body data-assets> and serves
// them as immutable. Each preload uses the request mode of the later real
// load (CORS for modules and fetch()), or the cache entry would not match.
function warm() {
    for (const url of (document.body.dataset.assets || "").split(" ")) {
        if (!url) continue;
        const link = document.createElement("link");
        if (url.includes(".json?")) {
            link.rel = "prefetch";
            link.crossOrigin = "anonymous";
        } else {
            link.rel = "modulepreload";
        }
        link.href = url;
        document.head.append(link);
    }
}

if (document.readyState === "complete") warm();
else addEventListener("load", warm, { once: true });
