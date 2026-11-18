// Copy a block's image to the clipboard. Our right-click menu replaces the
// browser's, which took its "Copy Image" with it, so this puts it back.
//
// The clipboard only reliably takes image/png, so the image is drawn into a
// canvas and re-encoded. That needs CORS: images.are.na sends
// Access-Control-Allow-Origin: *, so a crossOrigin load keeps the canvas clean.
//
// The ClipboardItem gets a promise rather than a blob so the write starts
// inside the click (Safari drops clipboard writes made after an await).
export default function copyImage(thumb) {
    const png = toPng(largerUrl(thumb)).catch(() => toPng(thumb));
    return navigator.clipboard.write([new ClipboardItem({ "image/png": png })]);
}

// Thumbs are images.are.na URLs: base64 JSON naming the original plus edits
// (a 500px resize). Ask for up to 2000px instead. The untouched original can
// be huge (over ~6MB it's refused with a 413), so it stays resized.
function largerUrl(thumb) {
    try {
        const u = new URL(thumb);
        if (u.hostname !== "images.are.na") return thumb;
        const spec = JSON.parse(atob(u.pathname.slice(1)));
        spec.edits = { resize: { width: 2000, height: 2000, fit: "inside", withoutEnlargement: true } };
        u.pathname = "/" + btoa(JSON.stringify(spec));
        return u.href;
    } catch {
        return thumb;
    }
}

async function toPng(src) {
    const img = new Image();
    img.crossOrigin = "anonymous";
    img.src = src;
    await img.decode();
    const canvas = document.createElement("canvas");
    canvas.width = img.naturalWidth;
    canvas.height = img.naturalHeight;
    canvas.getContext("2d").drawImage(img, 0, 0);
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
    if (!blob) throw new Error("png encode failed");
    return blob;
}
