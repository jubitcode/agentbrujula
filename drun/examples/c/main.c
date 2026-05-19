#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    char name[64];
    int value;
} Item;

void print_item(const Item *item) {
    printf("  %s: %d\n", item->name, item->value);
}

int main(void) {
    printf("Hello from C!\n\n");

    // Print compiler info
    printf("Compiler: GCC %d.%d.%d\n",
           __GNUC__, __GNUC_MINOR__, __GNUC_PATCHLEVEL__);
    printf("C Standard: %ld\n\n", __STDC_VERSION__);

    // Demonstrate dynamic allocation
    Item *items = malloc(3 * sizeof(Item));
    if (!items) {
        fprintf(stderr, "Memory allocation failed\n");
        return 1;
    }

    strcpy(items[0].name, "Alpha");
    items[0].value = 100;

    strcpy(items[1].name, "Beta");
    items[1].value = 200;

    strcpy(items[2].name, "Gamma");
    items[2].value = 300;

    printf("Items:\n");
    for (int i = 0; i < 3; i++) {
        print_item(&items[i]);
    }

    // Calculate sum
    int sum = 0;
    for (int i = 0; i < 3; i++) {
        sum += items[i].value;
    }
    printf("\nSum of values: %d\n", sum);

    free(items);
    return 0;
}
