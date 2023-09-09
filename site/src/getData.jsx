import { useState, useEffect, useCallback, useMemo } from "react";
import seedrandom from "seedrandom";

export default function useGetData(id, colCount) {
    const [lastItem, setLastItem] = useState(null);
    const [offset, setOffset] = useState(0);

    const [images, setImages] = useState([]);
    const [freshImages, setFreshImages] = useState([]);
    const [columns, setColumns] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);

    useEffect(() => {
        setLoading(true);
        console.log("fetching: " + id + " offset: " + offset + " limit: " + 8 * colCount)
        let multiplier = id ? 8 : 4;
        fetch(
            "https://river.maxbittker.com/neighbors?" +
            new URLSearchParams({
                id: id,
                offset: offset,
                limit: multiplier * colCount
            }),
        )
            .then((res) => res.json())
            .then((newData) => {
                let newImages = newData.images;
                if (!newImages) {
                    return
                }
                shuffleArray(newImages);
                window.count = 10;
                setImages([...images, ...newImages]);
                setFreshImages([...freshImages, ...newImages])
                setError(null);
            })
            .finally(() => setLoading(false));

        // .catch((err) => {
        //     console.error(err);
        //     setError(err);
        // })

        return () => {
            setLoading(false);
        };
    }, [id, offset, colCount]);

    useEffect(() => {
        if (freshImages.length === 0) return
        let remnantColumns = columns;
        if (offset === 0) {
            remnantColumns = columns.map((c, i) => {
                if (i !== lastItem?.x) {
                    return []
                }
                if (window.scrollY > 0) {
                    lastItem.y = 0;
                    return [lastItem]
                }
                return c.slice(0, lastItem.y + 1);
            });
            window.scrollTo(0, 0);
        }
        // console.log(remnantColumns, freshImages.length)
        let newColumns = collateListBalanced(
            freshImages,
            colCount,
            remnantColumns,
        );
        setFreshImages([])
        setColumns(newColumns)

    }, [freshImages, columns, offset, colCount, lastItem])

    let reset = useCallback(() => {
        setOffset(0)
        setImages([])
    }, [setOffset]);

    let loadMore = useCallback(() => {
        setOffset(images.length)
    }, [setOffset, images]);
    return { columns, loading, error, setLastItem, reset, loadMore };
}



function collateListBalanced(newItems, columns, oldColumns) {
    let skipList = {};
    let heights = Array(columns).fill(0);
    if (oldColumns) {
        heights = heights.map(
            (h, i) =>
                oldColumns[i]?.reduce((a, b) => {
                    skipList[b.Id] = true;
                    return a + b?.Height / b?.Width;
                }, 0) ?? 0,
        );
    }

    let result = Array.from({ length: columns }, () => []);
    if (oldColumns) {
        oldColumns.forEach((list, i) => {
            result[i] = list;
        });
    }
    let list = newItems.filter((item) => !skipList[item.Id]);
    let i = 0;
    while (i < list.length) {
        let item = list[i];
        let minHeight = Math.min(...heights); // Find the smallest height
        let index = heights.findIndex((height) => height === minHeight); // first column w smallest height
        i++;
        let aspectRatio = item.Height / item.Width;
        result[index].push(item); // Push this item into the column with the smallest height
        heights[index] += aspectRatio; // Increase the total height of this column
    }

    return result;
}

function shuffleArray(array) {
    let rng = seedrandom(77);
    for (let i = array.length - 1; i > 0; i--) {
        const j = Math.floor(rng() * (i + 1));
        [array[i], array[j]] = [array[j], array[i]];
    }
}
