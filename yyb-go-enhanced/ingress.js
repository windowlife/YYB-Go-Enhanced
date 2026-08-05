(() => {
    "use strict";

    const currentScript =
        document.currentScript ||
        Array.from(document.scripts).find((item) =>
            item.src.endsWith("/_yyb_ingress.js")
        );

    if (!currentScript) {
        return;
    }

    const scriptUrl = new URL(currentScript.src, window.location.href);
    const scriptName = "_yyb_ingress.js";

    let basePath = scriptUrl.pathname.slice(
        0,
        scriptUrl.pathname.length - scriptName.length
    );

    if (!basePath.endsWith("/")) {
        basePath += "/";
    }

    function appUrl(value) {
        if (typeof value !== "string" || !value.startsWith("/")) {
            return value;
        }

        // 普通 Docker 访问时不需要修改。
        if (basePath === "/") {
            return value;
        }

        // 已经带有当前 Ingress 前缀。
        if (value.startsWith(basePath)) {
            return value;
        }

        return basePath + value.replace(/^\/+/, "");
    }

    window.YYB_APP_URL = appUrl;

    /*
     * 兼容：
     * fetch("/accounts")
     * fetch(`/qr/${sid}/poll`)
     */
    const originalFetch = window.fetch.bind(window);

    window.fetch = function patchedFetch(input, init) {
        if (typeof input === "string") {
            input = appUrl(input);
        } else if (input instanceof Request) {
            const originalUrl = new URL(input.url, window.location.href);

            if (originalUrl.origin === window.location.origin) {
                const convertedPath = appUrl(originalUrl.pathname);

                if (convertedPath !== originalUrl.pathname) {
                    const convertedUrl =
                        window.location.origin +
                        convertedPath +
                        originalUrl.search +
                        originalUrl.hash;

                    input = new Request(convertedUrl, input);
                }
            }
        }

        return originalFetch(input, init);
    };

    /*
     * Swagger UI 或其他页面可能使用 XMLHttpRequest。
     */
    const originalXhrOpen = XMLHttpRequest.prototype.open;

    XMLHttpRequest.prototype.open = function patchedOpen(
        method,
        url,
        ...args
    ) {
        if (typeof url === "string") {
            url = appUrl(url);
        }

        return originalXhrOpen.call(this, method, url, ...args);
    };

    function rewriteAttribute(element, attributeName) {
        const value = element.getAttribute(attributeName);

        if (
            !value ||
            !value.startsWith("/") ||
            basePath === "/" ||
            value.startsWith(basePath)
        ) {
            return;
        }

        element.setAttribute(attributeName, appUrl(value));
    }

    const attributeRules = [
        ['a[href^="/"]', "href"],
        ['link[href^="/"]', "href"],
        ['img[src^="/"]', "src"],
        ['script[src^="/"]', "src"],
        ['iframe[src^="/"]', "src"],
        ['form[action^="/"]', "action"]
    ];

    function rewriteTree(root) {
        if (!root) {
            return;
        }

        for (const [selector, attributeName] of attributeRules) {
            if (root.matches && root.matches(selector)) {
                rewriteAttribute(root, attributeName);
            }

            if (root.querySelectorAll) {
                root.querySelectorAll(selector).forEach((element) => {
                    rewriteAttribute(element, attributeName);
                });
            }
        }
    }

    function beginRewrite() {
        rewriteTree(document);

        const observer = new MutationObserver((mutations) => {
            for (const mutation of mutations) {
                if (mutation.type === "attributes") {
                    rewriteTree(mutation.target);
                    continue;
                }

                mutation.addedNodes.forEach((node) => {
                    if (node.nodeType === Node.ELEMENT_NODE) {
                        rewriteTree(node);
                    }
                });
            }
        });

        observer.observe(document.documentElement, {
            subtree: true,
            childList: true,
            attributes: true,
            attributeFilter: ["href", "src", "action"]
        });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", beginRewrite, {
            once: true
        });
    } else {
        beginRewrite();
    }

    /*
     * scan.html 当前使用 location.href = "/" 返回首页。
     * 在捕获阶段处理，避免跳到 Home Assistant 根页面。
     */
    document.addEventListener(
        "click",
        (event) => {
            const anchor = event.target.closest?.('a[href^="/"]');

            if (anchor) {
                rewriteAttribute(anchor, "href");
            }

            const backButton = event.target.closest?.("#backBtn");

            if (backButton && basePath !== "/") {
                event.preventDefault();
                event.stopImmediatePropagation();
                window.location.assign(basePath);
            }
        },
        true
    );
})();
