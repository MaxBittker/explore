import { useEffect, useRef, useState } from "react";
import { ImageIcon, Cross2Icon, MagnifyingGlassIcon } from "@radix-ui/react-icons";
import { imageFileFrom } from "./searchApi";

// Search box: type to search by text, or pick / drop / paste an image.
// Collapsed to a single button until clicked (or while a search is active).
export default function Search({ q, imageThumb, pending, error, onText, onImage, onClear }) {
    const [text, setText] = useState(q || "");
    const [open, setOpen] = useState(false);
    const [dragging, setDragging] = useState(false);
    const fileRef = useRef(null);
    const inputRef = useRef(null);

    useEffect(() => setText(q || ""), [q]);

    const expanded = open || dragging || pending || !!q || !!imageThumb;

    useEffect(() => {
        if (open) inputRef.current?.focus();
    }, [open]);

    // Collapse when focus leaves the box with nothing typed. Ignore blurs
    // caused by the window losing focus (e.g. the file picker opening).
    const collapseIfIdle = (e) => {
        if (e.currentTarget.contains(e.relatedTarget)) return;
        if (!document.hasFocus()) return;
        if (!text.trim()) setOpen(false);
    };

    // Accept images dropped or pasted anywhere on the page.
    useEffect(() => {
        const hasFiles = (e) => [...(e.dataTransfer?.types || [])].includes("Files");
        const onDragOver = (e) => {
            if (!hasFiles(e)) return;
            e.preventDefault();
            setDragging(true);
        };
        const onDragLeave = (e) => {
            if (!e.relatedTarget) setDragging(false);
        };
        const onDrop = (e) => {
            setDragging(false);
            if (!hasFiles(e)) return;
            e.preventDefault(); // don't navigate away to the dropped file
            const file = imageFileFrom(e.dataTransfer);
            if (file) onImage(file);
        };
        const onPaste = (e) => {
            const file = imageFileFrom(e.clipboardData);
            if (!file) return;
            e.preventDefault();
            onImage(file);
        };
        window.addEventListener("dragover", onDragOver);
        window.addEventListener("dragleave", onDragLeave);
        window.addEventListener("drop", onDrop);
        window.addEventListener("paste", onPaste);
        return () => {
            window.removeEventListener("dragover", onDragOver);
            window.removeEventListener("dragleave", onDragLeave);
            window.removeEventListener("drop", onDrop);
            window.removeEventListener("paste", onPaste);
        };
    }, [onImage]);

    const fileInput = (
        <input
            ref={fileRef}
            type="file"
            accept="image/*"
            hidden
            onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = "";
                if (file) onImage(file);
            }}
        />
    );

    return (
        <div className="search-wrap">
            {expanded ? (
                <form
                    className={
                        "search" +
                        (dragging ? " dragging" : "") +
                        (pending ? " pending" : "") +
                        (error ? " error" : "")
                    }
                    title={error || undefined}
                    aria-busy={pending || undefined}
                    onBlur={collapseIfIdle}
                    onKeyDown={(e) => {
                        if (e.key === "Escape" && !text.trim()) {
                            setOpen(false);
                            inputRef.current?.blur();
                        }
                    }}
                    onSubmit={(e) => {
                        e.preventDefault();
                        const t = text.trim();
                        if (t) onText(t);
                        inputRef.current?.blur();
                    }}
                >
                    {imageThumb && (
                        <span className="search-chip">
                            <img src={imageThumb} alt="image query" />
                            <button
                                type="button"
                                title="clear search"
                                onClick={() => {
                                    setOpen(false);
                                    onClear();
                                }}
                            >
                                <Cross2Icon width={10} height={10} />
                            </button>
                        </span>
                    )}
                    <input
                        ref={inputRef}
                        type="search"
                        enterKeyHint="search"
                        placeholder={dragging ? "drop image" : imageThumb ? "" : "search"}
                        aria-label="search by text or image"
                        value={text}
                        onChange={(e) => setText(e.target.value)}
                    />
                    <button
                        type="button"
                        className="search-image"
                        title="search by image"
                        // keep focus in the input so the box doesn't collapse mid-click
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => fileRef.current?.click()}
                    >
                        <ImageIcon width={18} height={18} />
                    </button>
                </form>
            ) : (
                <button className="search-toggle" title="search" onClick={() => setOpen(true)}>
                    <MagnifyingGlassIcon width={24} height={24} />
                </button>
            )}
            {fileInput}
            {/* status for screen readers only; sighted users get the in-box cues */}
            <span className="visually-hidden" role="status">
                {error || (pending ? "searching" : "")}
            </span>
        </div>
    );
}
