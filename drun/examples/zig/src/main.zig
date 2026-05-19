const std = @import("std");

pub fn main() !void {
    const stdout = std.io.getStdOut().writer();

    try stdout.print("Hello from Zig!\n", .{});
    try stdout.print("Zig version: {}\n", .{@import("builtin").zig_version});

    // Demonstrate some Zig features
    const numbers = [_]i32{ 1, 2, 3, 4, 5 };
    var sum: i32 = 0;
    for (numbers) |n| {
        sum += n;
    }
    try stdout.print("Sum of array = {}\n", .{sum});

    // Comptime features
    const factorial = comptime blk: {
        var result: i32 = 1;
        for (1..6) |i| {
            result *= @intCast(i);
        }
        break :blk result;
    };
    try stdout.print("5! (computed at compile time) = {}\n", .{factorial});
}

test "basic sum" {
    const numbers = [_]i32{ 1, 2, 3, 4, 5 };
    var sum: i32 = 0;
    for (numbers) |n| {
        sum += n;
    }
    try std.testing.expectEqual(@as(i32, 15), sum);
}
