/**
 * Self-contained, zero-dependency QR Code generator (Byte mode, Error Correction Level L/M).
 * Generates an SVG string / boolean matrix suitable for crisp rendering on any display.
 */

// GF(256) log and exp tables for Reed-Solomon coding
const EXP_TABLE = new Uint8Array(512);
const LOG_TABLE = new Uint8Array(256);

(() => {
	let x = 1;
	for (let i = 0; i < 255; i++) {
		EXP_TABLE[i] = x;
		EXP_TABLE[i + 255] = x;
		LOG_TABLE[x] = i;
		x = (x << 1) ^ (x >= 128 ? 0x11d : 0);
	}
})();

function gmult(a: number, b: number): number {
	if (a === 0 || b === 0) return 0;
	return EXP_TABLE[LOG_TABLE[a] + LOG_TABLE[b]];
}

function polyMul(p1: Uint8Array, p2: Uint8Array): Uint8Array {
	const res = new Uint8Array(p1.length + p2.length - 1);
	for (let i = 0; i < p1.length; i++) {
		for (let j = 0; j < p2.length; j++) {
			res[i + j] ^= gmult(p1[i], p2[j]);
		}
	}
	return res;
}

function getGeneratorPoly(numEc: number): Uint8Array {
	let gen = new Uint8Array([1]);
	for (let i = 0; i < numEc; i++) {
		gen = polyMul(gen, new Uint8Array([1, EXP_TABLE[i]]));
	}
	return gen;
}

function rsEncode(data: Uint8Array, numEc: number): Uint8Array {
	const gen = getGeneratorPoly(numEc);
	const res = new Uint8Array(data.length + numEc);
	res.set(data);
	for (let i = 0; i < data.length; i++) {
		const coef = res[i];
		if (coef !== 0) {
			for (let j = 0; j < gen.length; j++) {
				res[i + j] ^= gmult(gen[j], coef);
			}
		}
	}
	return res.slice(data.length);
}

// Table of QR versions: total codewords, EC codewords for Level L & M, and alignment pattern coordinates
interface VersionInfo {
	version: number;
	totalCodewords: number;
	ecCodewordsL: number;
	ecCodewordsM: number;
	numBlocksL: number;
	numBlocksM: number;
	alignments: number[];
}

const VERSION_TABLE: VersionInfo[] = [
	{
		version: 1,
		totalCodewords: 26,
		ecCodewordsL: 7,
		ecCodewordsM: 10,
		numBlocksL: 1,
		numBlocksM: 1,
		alignments: [],
	},
	{
		version: 2,
		totalCodewords: 44,
		ecCodewordsL: 10,
		ecCodewordsM: 16,
		numBlocksL: 1,
		numBlocksM: 1,
		alignments: [6, 18],
	},
	{
		version: 3,
		totalCodewords: 70,
		ecCodewordsL: 15,
		ecCodewordsM: 26,
		numBlocksL: 1,
		numBlocksM: 1,
		alignments: [6, 22],
	},
	{
		version: 4,
		totalCodewords: 100,
		ecCodewordsL: 20,
		ecCodewordsM: 36,
		numBlocksL: 1,
		numBlocksM: 2,
		alignments: [6, 26],
	},
	{
		version: 5,
		totalCodewords: 134,
		ecCodewordsL: 26,
		ecCodewordsM: 48,
		numBlocksL: 1,
		numBlocksM: 2,
		alignments: [6, 30],
	},
	{
		version: 6,
		totalCodewords: 172,
		ecCodewordsL: 36,
		ecCodewordsM: 64,
		numBlocksL: 2,
		numBlocksM: 4,
		alignments: [6, 34],
	},
	{
		version: 7,
		totalCodewords: 196,
		ecCodewordsL: 40,
		ecCodewordsM: 72,
		numBlocksL: 2,
		numBlocksM: 4,
		alignments: [6, 22, 38],
	},
	{
		version: 8,
		totalCodewords: 242,
		ecCodewordsL: 48,
		ecCodewordsM: 88,
		numBlocksL: 2,
		numBlocksM: 4,
		alignments: [6, 24, 42],
	},
	{
		version: 9,
		totalCodewords: 292,
		ecCodewordsL: 60,
		ecCodewordsM: 110,
		numBlocksL: 2,
		numBlocksM: 5,
		alignments: [6, 26, 46],
	},
	{
		version: 10,
		totalCodewords: 346,
		ecCodewordsL: 72,
		ecCodewordsM: 130,
		numBlocksL: 4,
		numBlocksM: 6,
		alignments: [6, 28, 50],
	},
];

export function generateQrMatrix(text: string): boolean[][] {
	const encoder = new TextEncoder();
	const bytes = encoder.encode(text);
	const dataLength = bytes.length;

	// Pick lowest version that can hold data in Byte mode (Level L or M)
	let verInfo: VersionInfo | null = null;
	for (const v of VERSION_TABLE) {
		const dataCapacity = v.totalCodewords - v.ecCodewordsM;
		// 4 bits mode + 8 bits char count + 8*dataLength <= capacity * 8
		if (dataLength + 2 <= dataCapacity) {
			verInfo = v;
			break;
		}
	}

	if (!verInfo) {
		// Fallback to highest supported or Level L
		verInfo = VERSION_TABLE[VERSION_TABLE.length - 1];
	}

	const version = verInfo.version;
	const size = version * 4 + 17;
	const totalDataCodewords = verInfo.totalCodewords - verInfo.ecCodewordsM;

	// Build bit stream
	const bits: number[] = [];
	const pushBits = (val: number, len: number) => {
		for (let i = len - 1; i >= 0; i--) {
			bits.push((val >> i) & 1);
		}
	};

	// Mode: Byte (0100)
	pushBits(0b0100, 4);
	// Character count indicator (8 bits for v1-9, 16 bits for v10+)
	pushBits(dataLength, version <= 9 ? 8 : 16);
	// Data bytes
	for (let i = 0; i < dataLength; i++) {
		pushBits(bytes[i], 8);
	}
	// Terminator (up to 4 bits)
	const maxBits = totalDataCodewords * 8;
	const termLen = Math.min(4, maxBits - bits.length);
	pushBits(0, termLen);

	// Pad to byte boundary
	while (bits.length % 8 !== 0) {
		bits.push(0);
	}

	// Pad with 0xEC and 0x11
	const padBytes = [0xec, 0x11];
	let padIdx = 0;
	while (bits.length < maxBits) {
		pushBits(padBytes[padIdx % 2], 8);
		padIdx++;
	}

	// Convert bits to data codewords
	const dataCodewords = new Uint8Array(totalDataCodewords);
	for (let i = 0; i < totalDataCodewords; i++) {
		let byteVal = 0;
		for (let b = 0; b < 8; b++) {
			byteVal = (byteVal << 1) | bits[i * 8 + b];
		}
		dataCodewords[i] = byteVal;
	}

	// RS error correction
	const numBlocks = verInfo.numBlocksM;
	const totalEc = verInfo.ecCodewordsM;
	const ecPerBlock = Math.floor(totalEc / numBlocks);
	const dataPerBlock = Math.floor(totalDataCodewords / numBlocks);

	const blockData: Uint8Array[] = [];
	const blockEc: Uint8Array[] = [];

	let offset = 0;
	for (let b = 0; b < numBlocks; b++) {
		const blockLen = dataPerBlock + (b >= numBlocks - (totalDataCodewords % numBlocks) ? 1 : 0);
		const bData = dataCodewords.slice(offset, offset + blockLen);
		offset += blockLen;
		blockData.push(bData);
		blockEc.push(rsEncode(bData, ecPerBlock));
	}

	// Interleave codewords
	const finalCodewords: number[] = [];
	const maxBlockLen = Math.max(...blockData.map((b) => b.length));
	for (let i = 0; i < maxBlockLen; i++) {
		for (let b = 0; b < numBlocks; b++) {
			if (i < blockData[b].length) {
				finalCodewords.push(blockData[b][i]);
			}
		}
	}
	for (let i = 0; i < ecPerBlock; i++) {
		for (let b = 0; b < numBlocks; b++) {
			finalCodewords.push(blockEc[b][i]);
		}
	}

	// Remainder bits
	const allBits: number[] = [];
	for (const cw of finalCodewords) {
		for (let i = 7; i >= 0; i--) {
			allBits.push((cw >> i) & 1);
		}
	}

	// Build matrix
	const matrix: (boolean | null)[][] = Array.from({ length: size }, () => Array(size).fill(null));
	const isFunction: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false));

	const markFunction = (r: number, c: number, val: boolean) => {
		matrix[r][c] = val;
		isFunction[r][c] = true;
	};

	// 1. Finder patterns (7x7)
	const addFinder = (top: number, left: number) => {
		for (let r = 0; r < 7; r++) {
			for (let c = 0; c < 7; c++) {
				const isDark =
					r === 0 || r === 6 || c === 0 || c === 6 || (r >= 2 && r <= 4 && c >= 2 && c <= 4);
				markFunction(top + r, left + c, isDark);
			}
		}
		// Separators
		for (let r = -1; r <= 7; r++) {
			for (let c = -1; c <= 7; c++) {
				const row = top + r;
				const col = left + c;
				if (row >= 0 && row < size && col >= 0 && col < size && !isFunction[row][col]) {
					markFunction(row, col, false);
				}
			}
		}
	};

	addFinder(0, 0);
	addFinder(0, size - 7);
	addFinder(size - 7, 0);

	// 2. Alignment patterns
	const alignments = verInfo.alignments;
	for (const r of alignments) {
		for (const c of alignments) {
			if (isFunction[r][c]) continue;
			for (let dr = -2; dr <= 2; dr++) {
				for (let dc = -2; dc <= 2; dc++) {
					const isDark = Math.abs(dr) === 2 || Math.abs(dc) === 2 || (dr === 0 && dc === 0);
					markFunction(r + dr, c + dc, isDark);
				}
			}
		}
	}

	// 3. Timing patterns
	for (let i = 8; i < size - 8; i++) {
		if (!isFunction[6][i]) markFunction(6, i, i % 2 === 0);
		if (!isFunction[i][6]) markFunction(i, 6, i % 2 === 0);
	}

	// Dark module
	markFunction(size - 8, 8, true);

	// 4. Reserve format areas
	for (let i = 0; i < 9; i++) {
		if (!isFunction[8][i]) markFunction(8, i, false);
		if (!isFunction[i][8]) markFunction(i, 8, false);
	}
	for (let i = 0; i < 8; i++) {
		if (!isFunction[8][size - 1 - i]) markFunction(8, size - 1 - i, false);
		if (!isFunction[size - 1 - i][8]) markFunction(size - 1 - i, 8, false);
	}

	// 5. Data placement
	let bitIdx = 0;
	let upwards = true;

	for (let right = size - 1; right > 0; right -= 2) {
		if (right === 6) right--; // Skip vertical timing column
		const cols = [right, right - 1];

		for (let v = 0; v < size; v++) {
			const r = upwards ? size - 1 - v : v;
			for (const c of cols) {
				if (!isFunction[r][c]) {
					const bit = bitIdx < allBits.length ? allBits[bitIdx++] : 0;
					// Mask 0: (row + col) % 2 === 0
					const mask = (r + c) % 2 === 0;
					matrix[r][c] = (bit === 1) !== mask;
				}
			}
		}
		upwards = !upwards;
	}

	// 6. Write Format Info (Mask 0, EC M: 101010000010010 XOR 101010000010010 = 0x5412)
	// For EC M, Mask 0 -> 0x5412
	const formatBits = 0x5412;
	for (let i = 0; i < 15; i++) {
		const bit = ((formatBits >> (14 - i)) & 1) === 1;
		if (i <= 5) {
			matrix[8][i] = bit;
		} else if (i === 6) {
			matrix[8][7] = bit;
		} else if (i === 7) {
			matrix[8][8] = bit;
		} else if (i === 8) {
			matrix[7][8] = bit;
		} else {
			matrix[14 - i][8] = bit;
		}

		if (i < 8) {
			matrix[size - 1 - i][8] = bit;
		} else {
			matrix[8][size - 15 + i] = bit;
		}
	}

	return matrix.map((row) => row.map((cell) => cell === true));
}
