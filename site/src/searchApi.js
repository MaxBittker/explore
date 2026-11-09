import { useEffect, useReducer, useState, useCallback } from "react";
import { API_BASE } from "./config";

// Search queries are turned into embedding vectors by the server's /embed
// endpoint; results then page through /vectorNeighbors with that vector.

async function postEmbed(form) {
    const res = await fetch(API_BASE + "/embed", { method: "POST", body: form });
    const body = await res.json().catch(() => ({}));
    if (!res.ok || !body.vector) {
        throw new Error(body.message || "search failed");
    }
    return body.vector;
}

const textVectors = new Map();

// useTextVector embeds q (memoized for the session) and returns its vector
// once ready. retry() re-requests after an error.
export function useTextVector(q) {
    const [, rerender] = useReducer((x) => x + 1, 0);
    const [errors, setErrors] = useState({});
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        if (!q || textVectors.has(q)) return;
        let live = true;
        const form = new FormData();
        form.append("text", q);
        postEmbed(form)
            .then((v) => {
                textVectors.set(q, v);
                if (live) rerender();
            })
            .catch((e) => {
                if (live) setErrors((errs) => ({ ...errs, [q]: e.message }));
            });
        return () => {
            live = false;
        };
    }, [q, attempt]);

    const retry = useCallback(() => {
        setErrors((errs) => {
            const { [q]: _, ...rest } = errs;
            return rest;
        });
        setAttempt((a) => a + 1);
    }, [q]);

    return {
        vector: (q && textVectors.get(q)) || null,
        error: (q && errors[q]) || null,
        retry,
    };
}

// embedImage shrinks an image file client-side (SigLIP sees 384px anyway),
// embeds it, and returns { vector, thumb } where thumb is a tiny data URL
// for showing the query in the search box.
export async function embedImage(file) {
    let blob = file;
    let thumb = null;
    try {
        const bmp = await createImageBitmap(file);
        blob = await new Promise((resolve) => draw(bmp, 512).toBlob(resolve, "image/jpeg", 0.9));
        thumb = draw(bmp, 96).toDataURL("image/jpeg", 0.8);
    } catch {
        // The browser can't decode it (e.g. some SVGs); let the server try.
    }
    const form = new FormData();
    form.append("image", blob, "query.jpg");
    return { vector: await postEmbed(form), thumb };
}

function draw(bmp, maxSide) {
    const s = Math.min(1, maxSide / Math.max(bmp.width, bmp.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bmp.width * s));
    canvas.height = Math.max(1, Math.round(bmp.height * s));
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "#fff"; // flatten transparency onto white, not black
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(bmp, 0, 0, canvas.width, canvas.height);
    return canvas;
}

export function imageFileFrom(dataTransfer) {
    return [...(dataTransfer?.files || [])].find((f) => f.type.startsWith("image/")) || null;
}
