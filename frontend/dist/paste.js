// What a paste holds and how an image gets to the machine the agents run on.
// No DOM needed beyond what's passed in: paste.test.mjs runs it in Node.

// The types kept as they are; anything else (TIFF, BMP…) is converted.
const KEPT = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

// Base64 of this still fits a browser's 4 MB message (ws.go), and the server's limit.
export const MAX_IMAGE_BYTES = 2_900_000;

// imageIn finds the image to paste in a clipboardData. Text wins: copying
// from a spreadsheet or a word processor puts a picture of the text on the
// clipboard too, and it's the text you're after. A copied image has none.
export function imageIn(data) {
  if (!data || data.getData?.("text/plain")) return null;
  for (const item of data.items || []) {
    if (item.kind === "file" && item.type.startsWith("image/")) {
      const f = item.getAsFile();
      if (f) return f;
    }
  }
  return null;
}

// imageInItems is imageIn for navigator.clipboard.read(): null when the
// clipboard holds text, or no image.
export async function imageInItems(items) {
  if (items.some((i) => i.types.includes("text/plain"))) return null;
  for (const item of items) {
    const type = item.types.find((t) => t.startsWith("image/"));
    if (type) return item.getType(type);
  }
  return null;
}

// fitsAsIs says whether an image can be sent without converting it.
export const fitsAsIs = (blob) => blob.size <= MAX_IMAGE_BYTES && KEPT.has(blob.type);

// scaled is the size of an image with its long side at most maxSide.
export function scaled(width, height, maxSide) {
  const k = Math.min(1, maxSide / Math.max(width, height));
  return { width: Math.max(1, Math.round(width * k)), height: Math.max(1, Math.round(height * k)) };
}

// Each try is smaller than the last: a long side and a JPEG quality.
const TRIES = [[4096, 0.9], [2576, 0.85], [2000, 0.8], [1568, 0.75], [1024, 0.7]];

// shrink makes an image small enough to send: first the original pixels as
// PNG (for a type that wasn't kept), then smaller JPEGs. encode(side, quality)
// draws it and returns a Blob (no quality: PNG).
export async function shrink(encode) {
  const png = await encode(Infinity);
  if (png.size <= MAX_IMAGE_BYTES) return png;
  for (const [side, quality] of TRIES) {
    const b = await encode(side, quality);
    if (b.size <= MAX_IMAGE_BYTES) return b;
  }
  throw new Error("the image is too big to paste");
}

// imageForUpload is an image as base64, converted or shrunk if it must be.
export async function imageForUpload(blob) {
  let send = blob;
  if (!fitsAsIs(blob)) {
    const bitmap = await createImageBitmap(blob).catch(() => {
      throw new Error("this kind of image can't be pasted here");
    });
    try {
      send = await shrink((side, quality) => {
        const { width, height } = scaled(bitmap.width, bitmap.height, side);
        const canvas = document.createElement("canvas");
        canvas.width = width;
        canvas.height = height;
        canvas.getContext("2d").drawImage(bitmap, 0, 0, width, height);
        return new Promise((done, fail) =>
          canvas.toBlob((b) => (b ? done(b) : fail(new Error("the image couldn't be converted"))), quality ? "image/jpeg" : "image/png", quality));
      });
    } finally {
      bitmap.close();
    }
  }
  return base64(await send.arrayBuffer());
}

function base64(buffer) {
  const bytes = new Uint8Array(buffer);
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}
