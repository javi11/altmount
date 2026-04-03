import { CronExpressionParser } from "cron-parser";

export type ScheduleType = "hourly" | "daily" | "weekly" | "custom";

export interface ScheduleState {
	type: ScheduleType;
	time: string;
	dayOfWeek: string;
	customExpr: string;
}

export const DAY_NAMES = [
	"Sunday",
	"Monday",
	"Tuesday",
	"Wednesday",
	"Thursday",
	"Friday",
	"Saturday",
];

export function parseCronString(schedule: string): ScheduleState {
	try {
		const { fields } = CronExpressionParser.parse(schedule);
		const { minute, hour, dayOfMonth, month, dayOfWeek } = fields;

		// Only detect simple patterns when dom and month are wildcards
		if (dayOfMonth.values.length !== 31 || month.values.length !== 12) {
			return { type: "custom", time: "03:00", dayOfWeek: "1", customExpr: schedule };
		}

		const isDowWild = dayOfWeek.values.length === 8;

		// Hourly: 0 * * * *
		if (
			minute.values.length === 1 &&
			minute.values[0] === 0 &&
			hour.values.length === 24 &&
			isDowWild
		) {
			return { type: "hourly", time: "00:00", dayOfWeek: "1", customExpr: schedule };
		}

		// Daily or weekly: single hour + single minute
		if (hour.values.length === 1 && minute.values.length === 1) {
			const h = String(hour.values[0]).padStart(2, "0");
			const m = String(minute.values[0]).padStart(2, "0");
			const time = `${h}:${m}`;

			if (isDowWild) {
				return { type: "daily", time, dayOfWeek: "1", customExpr: schedule };
			}
			if (dayOfWeek.values.length === 1) {
				return {
					type: "weekly",
					time,
					dayOfWeek: String(dayOfWeek.values[0]),
					customExpr: schedule,
				};
			}
		}

		return { type: "custom", time: "03:00", dayOfWeek: "1", customExpr: schedule };
	} catch {
		return { type: "custom", time: "03:00", dayOfWeek: "1", customExpr: schedule };
	}
}

export function buildCronString(state: ScheduleState): string {
	if (state.type === "hourly") return "0 * * * *";
	if (state.type === "custom") return state.customExpr;
	const [hh, mm] = state.time.split(":");
	const h = Number.parseInt(hh, 10);
	const m = Number.parseInt(mm, 10);
	if (state.type === "daily") return `${m} ${h} * * *`;
	return `${m} ${h} * * ${state.dayOfWeek}`;
}
